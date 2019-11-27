// Copyright 2017 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bootstrap

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"path"
	"strings"
	"time"

	middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/hashicorp/go-multierror"
	prom "github.com/prometheus/client_golang/prometheus"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/pkg/ctrlz"
	"istio.io/pkg/env"
	"istio.io/pkg/filewatcher"
	"istio.io/pkg/log"
	"istio.io/pkg/version"

	"istio.io/istio/pilot/pkg/config/clusterregistry"
	"istio.io/istio/pilot/pkg/config/coredatamodel"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	envoyv2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schemas"
	istiokeepalive "istio.io/istio/pkg/keepalive"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/security/pkg/k8s/chiron"
)

var (
	// FilepathWalkInterval dictates how often the file system is walked for config
	FilepathWalkInterval = 100 * time.Millisecond

	// PilotCertDir is the default location for mTLS certificates used by pilot
	// Visible for tests - at runtime can be set by PILOT_CERT_DIR environment variable.
	PilotCertDir = "/etc/certs/"

	// DefaultPlugins is the default list of plugins to enable, when no plugin(s)
	// is specified through the command line
	DefaultPlugins = []string{
		plugin.Authn,
		plugin.Authz,
		plugin.Health,
		plugin.Mixer,
	}
)

func init() {
	// get the grpc server wired up
	// This should only be set before any RPCs are sent or received by this program.
	grpc.EnableTracing = true

	// Export pilot version as metric for fleet analytics.
	pilotVersion := prom.NewGaugeVec(prom.GaugeOpts{
		Name: "pilot_info",
		Help: "Pilot version and build information.",
	}, []string{"version"})
	prom.MustRegister(pilotVersion)
	pilotVersion.With(prom.Labels{"version": version.Info.String()}).Set(1)
}

// startFunc defines a function that will be used to start one or more components of the Pilot discovery service.
type startFunc func(stop <-chan struct{}) error

// Server contains the runtime configuration for the Pilot discovery service.
type Server struct {
	HTTPListeningAddr       net.Addr
	GRPCListeningAddr       net.Addr
	SecureGRPCListeningAddr net.Addr
	MonitorListeningAddr    net.Addr

	// TODO(nmittler): Consider alternatives to exposing these directly
	EnvoyXdsServer    *envoyv2.DiscoveryServer
	ServiceController *aggregate.Controller

	mesh             *meshconfig.MeshConfig
	meshNetworks     *meshconfig.MeshNetworks
	configController model.ConfigStoreCache

	kubeClient            kubernetes.Interface
	startFuncs            []startFunc
	multicluster          *clusterregistry.Multicluster
	httpServer            *http.Server
	grpcServer            *grpc.Server
	secureHTTPServer      *http.Server
	secureGRPCServer      *grpc.Server
	istioConfigStore      model.IstioConfigStore
	mux                   *http.ServeMux
	kubeRegistry          *kubecontroller.Controller
	fileWatcher           filewatcher.FileWatcher
	discoveryOptions      *coredatamodel.DiscoveryOptions
	mcpDiscovery          *coredatamodel.MCPDiscovery
	incrementalMcpOptions *coredatamodel.Options
	mcpOptions            *coredatamodel.Options
	certController        *chiron.WebhookController
}

var podNamespaceVar = env.RegisterStringVar("POD_NAMESPACE", "", "")

// NewServer creates a new Server instance based on the provided arguments.
func NewServer(args PilotArgs) (*Server, error) {
	// If the namespace isn't set, try looking it up from the environment.
	if args.Namespace == "" {
		args.Namespace = podNamespaceVar.Get()
	}
	if args.KeepaliveOptions == nil {
		args.KeepaliveOptions = istiokeepalive.DefaultOption()
	}
	if args.Config.ClusterRegistriesNamespace == "" {
		if args.Namespace != "" {
			args.Config.ClusterRegistriesNamespace = args.Namespace
		} else {
			args.Config.ClusterRegistriesNamespace = constants.IstioSystemNamespace
		}
	}

	s := &Server{
		fileWatcher: filewatcher.NewWatcher(),
	}

	prometheus.EnableHandlingTimeHistogram()

	// Apply the arguments to the configuration.
	if err := s.initKubeClient(&args); err != nil {
		return nil, fmt.Errorf("kube client: %v", err)
	}
	if err := s.initMesh(&args); err != nil {
		return nil, fmt.Errorf("mesh: %v", err)
	}
	if err := s.initMeshNetworks(&args); err != nil {
		return nil, fmt.Errorf("mesh networks: %v", err)
	}
	// Certificate controller is created before MCP
	// controller in case MCP server pod waits to mount a certificate
	// to be provisioned by the certificate controller.
	if err := s.initCertController(&args); err != nil {
		return nil, fmt.Errorf("certificate controller: %v", err)
	}
	if err := s.initConfigController(&args); err != nil {
		return nil, fmt.Errorf("config controller: %v", err)
	}
	if err := s.initServiceControllers(&args); err != nil {
		return nil, fmt.Errorf("service controllers: %v", err)
	}
	if err := s.initDiscoveryService(&args); err != nil {
		return nil, fmt.Errorf("discovery service: %v", err)
	}
	if err := s.initMonitor(args.DiscoveryOptions.MonitoringAddr); err != nil {
		return nil, fmt.Errorf("monitor: %v", err)
	}
	if err := s.initClusterRegistries(&args); err != nil {
		return nil, fmt.Errorf("cluster registries: %v", err)
	}
	if err := s.initSidecarInjector(&args); err != nil {
		return nil, fmt.Errorf("sidecar injector: %v", err)
	}

	if args.CtrlZOptions != nil {
		_, _ = ctrlz.Run(args.CtrlZOptions, nil)
	}

	return s, nil
}

// Start starts all components of the Pilot discovery service on the port specified in DiscoveryServiceOptions.
// If Port == 0, a port number is automatically chosen. Content serving is started by this method,
// but is executed asynchronously. Serving can be canceled at any time by closing the provided stop channel.
func (s *Server) Start(stop <-chan struct{}) error {
	// Now start all of the components.
	for _, fn := range s.startFuncs {
		if err := fn(stop); err != nil {
			return err
		}
	}

	return nil
}

// initKubeClient creates the k8s client if running in an k8s environment.
func (s *Server) initKubeClient(args *PilotArgs) error {
	if hasKubeRegistry(args.Service.Registries) && args.Config.FileDir == "" {
		client, kuberr := kubelib.CreateClientset(args.Config.KubeConfig, "")
		if kuberr != nil {
			return multierror.Prefix(kuberr, "failed to connect to Kubernetes API.")
		}
		s.kubeClient = client

	}

	return nil
}

func (s *Server) initDiscoveryService(args *PilotArgs) error {
	environment := &model.Environment{
		Mesh:             s.mesh,
		MeshNetworks:     s.meshNetworks,
		IstioConfigStore: s.istioConfigStore,
		ServiceDiscovery: s.ServiceController,
		PushContext:      model.NewPushContext(),
		PortManagerMap:   make(map[string][2]int),
	}

	s.initNsfEnviroment(args, environment)

	s.EnvoyXdsServer = envoyv2.NewDiscoveryServer(environment, args.Plugins)
	s.mux = http.NewServeMux()
	s.EnvoyXdsServer.InitDebug(s.mux, s.ServiceController, args.DiscoveryOptions.EnableProfiling)

	if err := s.initEventHandlers(); err != nil {
		return err
	}

	if s.kubeRegistry != nil {
		// kubeRegistry may use the environment for push status reporting.
		// TODO: maybe all registries should have this as an optional field ?
		s.kubeRegistry.Env = environment
		s.kubeRegistry.InitNetworkLookup(s.meshNetworks)
		s.kubeRegistry.XDSUpdater = s.EnvoyXdsServer
	}

	if s.mcpOptions != nil {
		s.mcpOptions.XDSUpdater = s.EnvoyXdsServer
	}
	if s.incrementalMcpOptions != nil {
		clusterID := args.Config.ControllerOptions.ClusterID
		s.incrementalMcpOptions.XDSUpdater = s.EnvoyXdsServer
		s.incrementalMcpOptions.ClusterID = clusterID
		s.discoveryOptions.Env = environment
		s.discoveryOptions.ClusterID = clusterID
	}

	// Implement EnvoyXdsServer grace shutdown
	s.addStartFunc(func(stop <-chan struct{}) error {
		s.EnvoyXdsServer.Start(stop)
		return nil
	})

	// create grpc/http server
	s.initGrpcServer(args.KeepaliveOptions)
	s.httpServer = &http.Server{
		Addr:    args.DiscoveryOptions.HTTPAddr,
		Handler: s.mux,
	}

	// create http listener
	listener, err := net.Listen("tcp", args.DiscoveryOptions.HTTPAddr)
	if err != nil {
		return err
	}
	s.HTTPListeningAddr = listener.Addr()

	// create grpc listener
	grpcListener, err := net.Listen("tcp", args.DiscoveryOptions.GrpcAddr)
	if err != nil {
		return err
	}
	s.GRPCListeningAddr = grpcListener.Addr()

	s.addStartFunc(func(stop <-chan struct{}) error {
		if !s.waitForCacheSync(stop) {
			return fmt.Errorf("failed to sync cache")
		}
		log.Infof("starting discovery service at http=%s grpc=%s", listener.Addr(), grpcListener.Addr())
		go func() {
			if err := s.httpServer.Serve(listener); err != nil {
				log.Warna(err)
			}
		}()
		go func() {
			if err := s.grpcServer.Serve(grpcListener); err != nil {
				log.Warna(err)
			}
		}()

		go func() {
			<-stop
			model.JwtKeyResolver.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err := s.httpServer.Shutdown(ctx)
			if err != nil {
				log.Warna(err)
			}
			if args.ForceStop {
				s.grpcServer.Stop()
			} else {
				s.grpcServer.GracefulStop()
			}
		}()

		return nil
	})

	// run secure grpc server
	if args.DiscoveryOptions.SecureGrpcAddr != "" {
		// create secure grpc server
		if err := s.initSecureGrpcServer(args.KeepaliveOptions); err != nil {
			return fmt.Errorf("secure grpc server: %s", err)
		}
		// create secure grpc listener
		secureGrpcListener, err := net.Listen("tcp", args.DiscoveryOptions.SecureGrpcAddr)
		if err != nil {
			return err
		}
		s.SecureGRPCListeningAddr = secureGrpcListener.Addr()

		s.addStartFunc(func(stop <-chan struct{}) error {
			go func() {
				if !s.waitForCacheSync(stop) {
					return
				}

				log.Infof("starting discovery service at secure grpc=%s", secureGrpcListener.Addr())
				go func() {
					// This seems the only way to call setupHTTP2 - it may also be possible to set NextProto
					// on a listener
					err := s.secureHTTPServer.ServeTLS(secureGrpcListener, "", "")
					msg := fmt.Sprintf("Stoppped listening on %s", secureGrpcListener.Addr().String())
					select {
					case <-stop:
						log.Info(msg)
					default:
						panic(fmt.Sprintf("%s due to error: %v", msg, err))
					}
				}()
				go func() {
					<-stop
					if args.ForceStop {
						s.grpcServer.Stop()
					} else {
						s.grpcServer.GracefulStop()
					}
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					_ = s.secureHTTPServer.Shutdown(ctx)
					s.secureGRPCServer.Stop()
				}()
			}()
			return nil
		})
	}

	return nil
}

func (s *Server) initGrpcServer(options *istiokeepalive.Options) {
	grpcOptions := s.grpcServerOptions(options)
	s.grpcServer = grpc.NewServer(grpcOptions...)
	s.EnvoyXdsServer.Register(s.grpcServer)
}

// initialize secureGRPCServer
func (s *Server) initSecureGrpcServer(options *istiokeepalive.Options) error {
	certDir := features.CertDir
	if certDir == "" {
		certDir = PilotCertDir
	}

	ca := path.Join(certDir, constants.RootCertFilename)
	key := path.Join(certDir, constants.KeyFilename)
	cert := path.Join(certDir, constants.CertChainFilename)

	tlsCreds, err := credentials.NewServerTLSFromFile(cert, key)
	// certs not ready yet.
	if err != nil {
		return err
	}

	// TODO: parse the file to determine expiration date. Restart listener before expiration
	certificate, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		return err
	}

	caCert, err := ioutil.ReadFile(ca)
	if err != nil {
		return err
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	opts := s.grpcServerOptions(options)
	opts = append(opts, grpc.Creds(tlsCreds))
	s.secureGRPCServer = grpc.NewServer(opts...)
	s.EnvoyXdsServer.Register(s.secureGRPCServer)
	s.secureHTTPServer = &http.Server{
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{certificate},
			VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
				// For now accept any certs - pilot is not authenticating the caller, TLS used for
				// privacy
				return nil
			},
			NextProtos: []string{"h2", "http/1.1"},
			ClientAuth: tls.RequireAndVerifyClientCert,
			ClientCAs:  caCertPool,
		},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ProtoMajor == 2 && strings.HasPrefix(
				r.Header.Get("Content-Type"), "application/grpc") {
				s.secureGRPCServer.ServeHTTP(w, r)
			} else {
				s.mux.ServeHTTP(w, r)
			}
		}),
	}

	return nil
}

func (s *Server) grpcServerOptions(options *istiokeepalive.Options) []grpc.ServerOption {
	interceptors := []grpc.UnaryServerInterceptor{
		// setup server prometheus monitoring (as final interceptor in chain)
		prometheus.UnaryServerInterceptor,
	}

	// Temp setting, default should be enough for most supported environments. Can be used for testing
	// envoy with lower values.
	maxStreams := features.MaxConcurrentStreams

	grpcOptions := []grpc.ServerOption{
		grpc.UnaryInterceptor(middleware.ChainUnaryServer(interceptors...)),
		grpc.MaxConcurrentStreams(uint32(maxStreams)),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:                  options.Time,
			Timeout:               options.Timeout,
			MaxConnectionAge:      options.MaxServerConnectionAge,
			MaxConnectionAgeGrace: options.MaxServerConnectionAgeGrace,
		}),
	}

	return grpcOptions
}

func (s *Server) addStartFunc(fn startFunc) {
	s.startFuncs = append(s.startFuncs, fn)
}

func (s *Server) waitForCacheSync(stop <-chan struct{}) bool {
	// TODO: remove dependency on k8s lib
	if !cache.WaitForCacheSync(stop, func() bool {
		if s.kubeRegistry != nil {
			if !s.kubeRegistry.HasSynced() {
				return false
			}
		}
		if !s.configController.HasSynced() {
			return false
		}
		return true
	}) {
		log.Errorf("Failed waiting for cache sync")
		return false
	}

	return true
}

// initEventHandlers sets up event handlers for config and service updates
func (s *Server) initEventHandlers() error {
	// Flush cached discovery responses whenever services configuration change.
	serviceHandler := func(svc *model.Service, _ model.Event) {
		pushReq := &model.PushRequest{
			Full:               true,
			NamespacesUpdated:  map[string]struct{}{svc.Attributes.Namespace: {}},
			ConfigTypesUpdated: map[string]struct{}{schemas.ServiceEntry.Type: {}},
		}
		s.EnvoyXdsServer.ConfigUpdate(pushReq)
	}
	if err := s.ServiceController.AppendServiceHandler(serviceHandler); err != nil {
		return fmt.Errorf("append service handler failed: %v", err)
	}

	instanceHandler := func(si *model.ServiceInstance, _ model.Event) {
		// TODO: This is an incomplete code. This code path is called for service entries, consul, etc.
		// In all cases, this is simply an instance update and not a config update. So, we need to update
		// EDS in all proxies, and do a full config push for the instance that just changed (add/update only).
		s.EnvoyXdsServer.ConfigUpdate(&model.PushRequest{
			Full:              true,
			NamespacesUpdated: map[string]struct{}{si.Service.Attributes.Namespace: {}},
			// TODO: extend and set service instance type, so no need re-init push context
			ConfigTypesUpdated: map[string]struct{}{schemas.ServiceEntry.Type: {}},
		})
	}
	if err := s.ServiceController.AppendInstanceHandler(instanceHandler); err != nil {
		return fmt.Errorf("append instance handler failed: %v", err)
	}

	// TODO(Nino-k): remove this case once incrementalUpdate is default
	if s.configController != nil {
		// TODO: changes should not trigger a full recompute of LDS/RDS/CDS/EDS
		// (especially mixerclient HTTP and quota)
		configHandler := func(c model.Config, _ model.Event) {
			pushReq := &model.PushRequest{
				Full:               true,
				ConfigTypesUpdated: map[string]struct{}{c.Type: {}},
			}
			s.EnvoyXdsServer.ConfigUpdate(pushReq)
		}
		for _, descriptor := range schemas.Istio {
			s.configController.RegisterEventHandler(descriptor.Type, configHandler)
		}
	}

	return nil
}

func grpcDial(ctx context.Context, cancel context.CancelFunc,
	configSource *meshconfig.ConfigSource, args *PilotArgs) (conn *grpc.ClientConn, err error) {
	securityOption, err := mcpSecurityOptions(ctx, cancel, configSource)
	if err != nil {
		return nil, err
	}

	keepaliveOption := grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:    args.KeepaliveOptions.Time,
		Timeout: args.KeepaliveOptions.Timeout,
	})

	initialWindowSizeOption := grpc.WithInitialWindowSize(int32(args.MCPInitialWindowSize))
	initialConnWindowSizeOption := grpc.WithInitialConnWindowSize(int32(args.MCPInitialConnWindowSize))
	msgSizeOption := grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(args.MCPMaxMessageSize))

	return grpc.DialContext(
		ctx,
		configSource.Address,
		securityOption,
		msgSizeOption,
		keepaliveOption,
		initialWindowSizeOption,
		initialConnWindowSizeOption)
}
