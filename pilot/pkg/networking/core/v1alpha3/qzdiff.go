package v1alpha3

import (
	//"fmt"
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	listener "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	//"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	fileaccesslog "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v2"
	accesslog "github.com/envoyproxy/go-control-plane/envoy/config/filter/accesslog/v2"
	//rate_limit_config "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/rate_limit/v2"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	//v2 "github.com/envoyproxy/go-control-plane/envoy/config/ratelimit/v2"
	//envoy_type "github.com/envoyproxy/go-control-plane/envoy/type"
	xdsutil "github.com/envoyproxy/go-control-plane/pkg/wellknown"
	//"github.com/gogo/protobuf/types"
	wrappers "github.com/golang/protobuf/ptypes/wrappers"
	//resty "istio.io/api/envoy/config/filter/http/resty/v2"
	//networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	//istio_route "istio.io/istio/pilot/pkg/networking/core/v1alpha3/route"
	//"istio.io/istio/pilot/pkg/networking/plugin/extension"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/pkg/log"
	"strconv"
	//"strings"
	//"time"
)

//func addGatewayHostLevelPlugin(virtualService model.Config, node *model.Proxy, vHost *route.VirtualHost) {
//	if node == nil {
//		return
//	}
//	for _, c11plugin := range extension.GetEnablePlugin() {
//		if message, ok := c11plugin.BuildHostLevelPlugin(virtualService.Spec.(*networking.VirtualService)); ok {
//			if util.IsXDSMarshalingToAnyEnabled(node) {
//				if vHost.TypedPerFilterConfig == nil {
//					vHost.TypedPerFilterConfig = make(map[string]*types.Any)
//				}
//				vHost.TypedPerFilterConfig[c11plugin.GetName()] = util.MessageToAny(message)
//			} else {
//				if vHost.PerFilterConfig == nil {
//					vHost.PerFilterConfig = make(map[string]*types.Struct)
//				}
//				vHost.PerFilterConfig[c11plugin.GetName()] = util.MessageToStruct(message)
//			}
//		}
//	}
//	for _, lua := range extension.GetLuaPlugin() {
//		if luaConfig, ok := lua.BuildHostLevelPlugin(virtualService.Spec.(*networking.VirtualService)); ok {
//			for k, v := range luaConfig {
//				if util.IsXDSMarshalingToAnyEnabled(node) {
//					if vHost.TypedPerFilterConfig == nil {
//						vHost.TypedPerFilterConfig = make(map[string]*types.Any)
//					}
//					vHost.TypedPerFilterConfig[k] = util.MessageToAny(v)
//				} else {
//					if vHost.PerFilterConfig == nil {
//						vHost.PerFilterConfig = make(map[string]*types.Struct)
//					}
//					vHost.PerFilterConfig[k] = util.MessageToStruct(v)
//				}
//			}
//		}
//	}
//}
//
//func addXYanxuanAppHeader(proxyLabels model.LabelsCollection, out *xdsapi.RouteConfiguration, node *model.Proxy) {
//	// add x-yanxuan-app header
//	label := func(proxyLabels model.LabelsCollection) string {
//		for _, v := range proxyLabels {
//			if l, ok := v["yanxuan/app"]; ok {
//				return l
//			}
//		}
//		return "anonymous"
//	}(proxyLabels)
//	out.RequestHeadersToAdd = make([]*core.HeaderValueOption, 1)
//	out.RequestHeadersToAdd[0] = &core.HeaderValueOption{
//		Header: &core.HeaderValue{
//			Key:   "x-yanxuan-app",
//			Value: label + "." + node.ConfigNamespace,
//		},
//	}
//}
//
//func addSuffixIfNecessary(push *model.PushContext, host string, virtualHostWrapper istio_route.VirtualHostWrapper, uniques map[string]struct{}, name string, node *model.Proxy, env *model.Environment, vh route.VirtualHost) (*route.VirtualHost, bool) {
//	var egressVh *route.VirtualHost
//	if push.Env.NsfHostSuffix != "" && isK8SSvcHost(host) {
//		serviceName := strings.Split(host, ".")[0]
//		namespace := strings.Split(host, ".")[1]
//		n := fmt.Sprintf("%s:%d", namespace+"."+serviceName+push.Env.NsfHostSuffix, virtualHostWrapper.Port)
//		if _, ok := uniques[n]; ok {
//			push.Add(model.DuplicatedDomains, name, node, fmt.Sprintf("duplicate domain from virtual service: %s, when add prefix and suffix", name))
//			log.Debugf("Dropping duplicate route entry %v.", n)
//			return nil, true
//		}
//		if serviceName == env.EgressDomain {
//			egressVh = &vh
//		}
//		vh.Domains = append(vh.Domains, namespace+"."+serviceName+push.Env.NsfHostSuffix)
//	}
//	return egressVh, false
//}
//
//func isK8SSvcHost(name string) bool {
//	strs := strings.Split(name, ".")
//	if len(strs) != 5 {
//		return false
//	}
//	if strs[2] != "svc" {
//		return false
//	}
//	return true
//}
//
func (configgen *ConfigGeneratorImpl) buildDefaultHttpPortMappingListener(srcPort int, dstPort int,
	env *model.Environment, node *model.Proxy, proxyInstances []*model.ServiceInstance) *xdsapi.Listener {
	log.Infof("default listener build start")
	httpOpts := &httpListenerOpts{
		useRemoteAddress: false,
		direction:        http_conn.HttpConnectionManager_Tracing_EGRESS,
		rds:              strconv.Itoa(dstPort),
	}
	opts := buildListenerOpts{
		env:            env,
		proxy:          node,
		proxyInstances: proxyInstances,
		bind:           WildcardAddress,
		port:           srcPort,
		filterChainOpts: []*filterChainOpts{{
			httpOpts: httpOpts,
		}},
		bindToPort:      true,
		skipUserFilters: true,
	}
	l := buildListener(opts)
	rds := &http_conn.HttpConnectionManager_Rds{
		Rds: &http_conn.Rds{
			ConfigSource: &core.ConfigSource{
				ConfigSourceSpecifier: &core.ConfigSource_Ads{
					Ads: &core.AggregatedConfigSource{},
				},
			},
			RouteConfigName: httpOpts.rds,
		},
	}
	filters := []*http_conn.HttpFilter{
		{Name: xdsutil.Router},
	}
	//urltransformers := make([]*http_conn.UrlTransformer, len(env.NsfUrlPrefix))
	//for i, value := range env.NsfUrlPrefix {
	//	urltransformers[i] = &http_conn.UrlTransformer{
	//		Prefix: value,
	//	}
	//}
	connectionManager := &http_conn.HttpConnectionManager{
		CodecType:      http_conn.HttpConnectionManager_AUTO,
		StatPrefix:     "http_default",
		RouteSpecifier: rds,
		HttpFilters:    filters,
		//UrlTransformer: urltransformers,
		UseRemoteAddress: &wrappers.BoolValue{
			Value: true,
		},
	}
	if env.Mesh.AccessLogFile != "" {
		fl := &fileaccesslog.FileAccessLog{
			Path: env.Mesh.AccessLogFile,
		}

		acc := &accesslog.AccessLog{
			Name: xdsutil.FileAccessLog,
		}

		if util.IsIstioVersionGE12(node) {
			buildAccessLog(node, fl, env)
		}

		if util.IsXDSMarshalingToAnyEnabled(node) {
			acc.ConfigType = &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(fl)}
		} else {
			acc.ConfigType = &accesslog.AccessLog_Config{Config: util.MessageToStruct(fl)}
		}
		connectionManager.AccessLog = []*accesslog.AccessLog{acc}
	}

	l.FilterChains[0].Filters = append(l.FilterChains[0].Filters, &listener.Filter{
		Name: xdsutil.HTTPConnectionManager,
		ConfigType: &listener.Filter_TypedConfig{
			TypedConfig: util.MessageToAny(connectionManager),
		},
	})

	return l
}
func (configgen *ConfigGeneratorImpl) addDefaultPort(env *model.Environment, node *model.Proxy, listeners []*xdsapi.Listener) []*xdsapi.Listener {
	proxyInstances := node.ServiceInstances
	//Todo: support other protocol
	for k, v := range env.PortManagerMap {
		switch k {
		case "http":
			l := configgen.buildDefaultHttpPortMappingListener(v[0], v[1], env, node, proxyInstances)
			listeners = append(listeners, l)
		}
	}
	return listeners
}

//
//func addFilters(isGateway bool, filters []*http_conn.HttpFilter, plugins []*networking.Plugin) []*http_conn.HttpFilter {
//	if isGateway {
//		// for rate limit service config
//		rateLimiterFiler := http_conn.HttpFilter{Name: xdsutil.HTTPRateLimit}
//		rateLimitService := v2.RateLimitServiceConfig{
//			GrpcService: &core.GrpcService{
//				TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
//					EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
//						ClusterName: "rate_limit_service",
//					},
//				},
//			},
//		}
//		rateLimitServiceConfig := rate_limit_config.RateLimit{
//			Domain:           "qingzhou",
//			RateLimitService: &rateLimitService,
//		}
//		rateLimiterFiler.ConfigType = &http_conn.HttpFilter_Config{Config: util.MessageToStruct(&rateLimitServiceConfig)}
//		isInLuaChain := false
//		currentLuaChain := resty.EnablePlugins{}
//		for _, v := range plugins {
//			if !v.Enable {
//				continue
//			}
//			if strings.HasPrefix(v.Name, "com.netease.resty.") {
//				pl := &resty.Plugin{
//					Name: strings.TrimPrefix(v.Name, "com.netease.resty."),
//				}
//				if v.Settings != nil {
//					pl.Config = v.Settings
//				}
//				if !isInLuaChain {
//					isInLuaChain = true
//					currentLuaChain.Plugins = make([]*resty.Plugin, 1)
//					currentLuaChain.Plugins[0] = pl
//				} else {
//					currentLuaChain.Plugins = append(currentLuaChain.Plugins, pl)
//				}
//			} else {
//				if isInLuaChain {
//					isInLuaChain = false
//					filters = append(filters, &http_conn.HttpFilter{
//						Name: "com.netease.resty",
//						ConfigType: &http_conn.HttpFilter_Config{
//							Config: util.MessageToStruct(&currentLuaChain),
//						},
//					})
//				}
//				pl := &http_conn.HttpFilter{
//					Name: v.Name,
//				}
//				if v.Settings != nil {
//					pl.ConfigType = &http_conn.HttpFilter_Config{
//						Config: v.Settings,
//					}
//				}
//				filters = append(filters, pl)
//			}
//		}
//		if isInLuaChain {
//			isInLuaChain = false
//			filters = append(filters, &http_conn.HttpFilter{
//				Name: "com.netease.resty",
//				ConfigType: &http_conn.HttpFilter_Config{
//					Config: util.MessageToStruct(&currentLuaChain),
//				},
//			})
//		}
//		filters = append(filters, &rateLimiterFiler, &http_conn.HttpFilter{Name: xdsutil.Router})
//	} else {
//		filters = append(filters,
//			&http_conn.HttpFilter{Name: xdsutil.CORS},
//			&http_conn.HttpFilter{Name: xdsutil.Fault},
//			&http_conn.HttpFilter{Name: xdsutil.Router},
//		)
//	}
//	return filters
//}
//
//func addAdditionalConnectionManagerConfigs(connectionManager *http_conn.HttpConnectionManager) {
//	connectionManager.AddUserAgent = &types.BoolValue{
//		Value: true,
//	}
//	connectionManager.UseRemoteAddress = &types.BoolValue{
//		Value: true,
//	}
//}
//
//func getLogPath(path string, node *model.Proxy, env *model.Environment) string {
//	var serviceName string
//	for _, v := range node.WorkloadLabels {
//		for lk, lv := range v {
//			if env.ServiceLabel == lk {
//				serviceName = lv
//			}
//		}
//	}
//	if serviceName != "" && strings.HasSuffix(path, "/") {
//		return path + serviceName + "-envoy-access.log"
//	}
//	return path
//}
//
//func transformRangeInt64(int64Range []*networking.HealthCheckInt64Range) []*envoy_type.Int64Range {
//	transform := func(in *networking.HealthCheckInt64Range) *envoy_type.Int64Range {
//		return &envoy_type.Int64Range{
//			Start: in.Start,
//			End:   in.End,
//		}
//	}
//	out := make([]*envoy_type.Int64Range, 0, len(int64Range))
//	for _, v := range int64Range {
//		out = append(out, transform(v))
//	}
//	return out
//}
//func applyHealthCheck(cluster *xdsapi.Cluster, healthCheck *networking.HealthCheck) {
//	if healthCheck != nil {
//		out := core.HealthCheck{
//			HealthChecker: &core.HealthCheck_HttpHealthCheck_{
//				HttpHealthCheck: &core.HealthCheck_HttpHealthCheck{
//					Host: healthCheck.Host,
//					Path: healthCheck.Path,
//				},
//			},
//		}
//
//		var timeout time.Duration
//		if healthCheck.Timeout != nil {
//			timeout = util.GogoDurationToDuration(healthCheck.Timeout)
//		} else {
//			timeout = time.Minute
//		}
//		out.Timeout = &timeout
//
//		if len(healthCheck.ExpectedStatuses) != 0 {
//			out.HealthChecker.(*core.HealthCheck_HttpHealthCheck_).HttpHealthCheck.ExpectedStatuses =
//				transformRangeInt64(healthCheck.ExpectedStatuses)
//		} else {
//			out.HealthChecker.(*core.HealthCheck_HttpHealthCheck_).HttpHealthCheck.ExpectedStatuses = []*envoy_type.Int64Range{
//				{Start: 200, End: 200},
//			}
//		}
//
//		var interval time.Duration
//		if healthCheck.Interval != nil {
//			interval = util.GogoDurationToDuration(healthCheck.Interval)
//		} else {
//			interval = time.Second
//		}
//		out.Interval = &interval
//
//		out.UnhealthyInterval = healthCheck.UnhealthyInterval
//
//		if healthCheck.HealthyThreshold != nil {
//			out.HealthyThreshold = healthCheck.HealthyThreshold
//		} else {
//			out.HealthyThreshold = &types.UInt32Value{
//				Value: 1,
//			}
//		}
//
//		if healthCheck.UnhealthyThreshold != nil {
//			out.UnhealthyThreshold = healthCheck.UnhealthyThreshold
//		} else {
//			out.UnhealthyThreshold = &types.UInt32Value{
//				Value: 1,
//			}
//		}
//		cluster.HealthChecks = []*core.HealthCheck{&out}
//	}
//}
