package bootstrap

import (
	"github.com/hashicorp/go-multierror"
	"istio.io/istio/pilot/pkg/model"
	rlslib "istio.io/istio/pkg/rls"
	"istio.io/pkg/log"
	"strconv"
	"strings"
	"time"
)

// initRlsClient creates the RLS client if running with rate limiter service enable.
//func (s *Server) initRlsClient(args *PilotArgs) error {
//	if args.RLSServerAddrs != nil && len(args.RLSServerAddrs) > 0 {
//		client, rlserr, ches := rlslib.CreateClientSet(args.RLSServerAddrs)
//		if rlserr != nil {
//			return multierror.Prefix(rlserr, "failed to connect to RLS server ")
//		}
//		s.rlsClientSet = client
//		stop := make(chan struct{})
//		for _, v := range ches {
//			go s.reconnect(args, v, stop)
//		}
//	}
//	return nil
//}
//func (s *Server) reconnect(args *PilotArgs, c <-chan struct{}, stop chan struct{}) {
//	select {
//	case <-c:
//		for {
//			if err := s.initRlsClient(args); err == nil {
//				break
//			}
//			<-time.NewTimer(5 * time.Second).C
//		}
//		for {
//			if s.configController != nil {
//				break
//			}
//			time.Sleep(15 * time.Second)
//		}
//		if configs, err := s.configController.List(model.SharedConfig.Type, model.NamespaceAll); err == nil {
//			if err := s.rlsClientSet.SyncAllRatelimitConfig(configs); err != nil {
//				log.Errorf("Sync Ratelimit Config failed, cause: %v", err)
//			}
//		} else {
//			log.Errorf("get config failed, cause: %v", err)
//		}
//		close(stop)
//	case <-stop:
//		return
//	}
//}

func (s *Server) initNsfEnviroment(args *PilotArgs, environment *model.Environment) {
	kvs := strings.Split(args.PortMappingManager, ",")
	for _, value := range kvs {
		protocol, src, dst := parsePortMapping(value)
		environment.PortManagerMap[protocol] = [2]int{src, dst}
	}
	vs := strings.Split(args.NsfUrlPrefix, ",")
	environment.NsfUrlPrefix = vs
	environment.NsfHostSuffix = args.NsfHostSuffix
	environment.EgressDomain = args.EgressDomain
}

func parsePortMapping(str string) (string, int, int) {
	defer func() {
		if err := recover(); err != nil {
			log.Errorf("Invalid port input: %v ,so skip", err)
		}
	}()
	per := strings.Split(str, "|")
	sd := strings.Split(per[1], ":")
	srcport, err := strconv.Atoi(sd[0])
	if err != nil {
		panic(err)
	}
	dstport, err := strconv.Atoi(sd[1])
	if err != nil {
		panic(err)
	}
	return per[0], srcport, dstport
}
