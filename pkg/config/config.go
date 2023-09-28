package config

import (
	"fmt"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/cache"

	"github.com/kelseyhightower/envconfig"

	ctrl "sigs.k8s.io/controller-runtime"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// manager:
// # healthProbePort identifies the port the Controller Manager will listen on. Used by liveness and readiness probes
// healthProbePort: 8081
// # metricsPort identifies the port on which Tyk Operator metrics are served
// metricsPort: 8080
// # webhookPort identifies the port on which webhooks are handled
// webhookPort: 9443
// leaderElection:
// leaderElect: true
// resourceName: 91ad8c6e.tyk.io

// ManagerOpts refers to all controller manager options that are going to be parsed via environment variable.
type ManagerOpts struct {
	HealthProbePort            int    `default:"8081"`
	MetricsPort                int    `default:"8080"`
	WebhookPort                int    `default:"9443"`
	LeaderElect                bool   `default:"true"`
	LeaderElectionResourceName string `default:"91ad8c6e.tyk.io"`
}

// ManagerOptions returns controller runtime manager Options that is populated by ManagerOpts read via environment
// variables.
//
// The flow is as follows; all environment variables, fields of ManagerOpts, will be declared
// via environment variable and LoadFromEnv will process environment variables and populate
// ManagerOpts struct accordingly. Then, ManagerOptions() method will generate controller runtime manager
// options required to start Tyk Operator manager.
func (o *ManagerOpts) ManagerOptions(scheme *runtime.Scheme) ctrl.Options {
	return ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: fmt.Sprintf(":%d", o.HealthProbePort),
		Metrics: server.Options{
			BindAddress: fmt.Sprintf(":%d", o.MetricsPort),
		},
		LeaderElection:   o.LeaderElect,
		LeaderElectionID: o.LeaderElectionResourceName,
	}
}

// LoadFromEnv reads all manager options from environment variables and returns ManagerOpts struct
// that is populated according to environment variables.
func LoadFromEnv() (*ManagerOpts, error) {
	opts := &ManagerOpts{}
	err := envconfig.Process("TYK_OPERATOR", opts)

	return opts, err
}

func SetCacheOptions(namespaces string, options *ctrl.Options) {
	defaultNamespaces := make(map[string]cache.Config)
	for _, v := range strings.Split(namespaces, ",") {
		defaultNamespaces[v] = cache.Config{}
	}

	options.Cache.DefaultNamespaces = defaultNamespaces
}
