/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"os"
	"strings"
	"time"

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/go-logr/zapr"
	"go.elastic.co/ecszap"
	uzap "go.uber.org/zap"

	namespacelabelv1alpha1 "namespacelabel.dana.io/api/v1alpha1"
	"namespacelabel.dana.io/internal/controller"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(namespacelabelv1alpha1.AddToScheme(scheme))
}

// nolint:gocyclo
func main() {
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var protectedPrefixes string
	var managedLabelsAnnotation string
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&protectedPrefixes, "protected-prefixes", "kubernetes.io/,k8s.io/",
		"Comma-separated list of protected label prefixes.")
	flag.StringVar(&managedLabelsAnnotation, "managed-labels-annotation", "namespacelabel.dana.io/managed-labels",
		"Annotation to track managed labels.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	// Allow overrides via environment variables
	if env := os.Getenv("PROTECTED_PREFIXES"); env != "" {
		protectedPrefixes = env
	}
	if env := os.Getenv("MANAGED_LABELS_ANNOTATION"); env != "" {
		managedLabelsAnnotation = env
	}

	encoderConfig := ecszap.NewDefaultEncoderConfig()
	level := uzap.InfoLevel
	if opts.Development {
		level = uzap.DebugLevel
	}
	core := ecszap.NewCore(encoderConfig, os.Stdout, level)
	logger := uzap.New(core, uzap.AddCaller())
	ctrl.SetLogger(zapr.NewLogger(logger))

	resyncPeriod := time.Minute
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "9204f17c.dana.io",
		Cache: cache.Options{
			SyncPeriod: &resyncPeriod,
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err := (&controller.NamespaceLabelReconciler{
		Client:                  mgr.GetClient(),
		Scheme:                  mgr.GetScheme(),
		ProtectedPrefixes:       strings.Split(protectedPrefixes, ","),
		ManagedLabelsAnnotation: managedLabelsAnnotation,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "NamespaceLabel")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
