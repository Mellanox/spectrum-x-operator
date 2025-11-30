/*
 Copyright 2025, NVIDIA CORPORATION & AFFILIATES

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
	"context"
	"crypto/tls"
	"flag"
	"os"
	"time"

	spectrumxv1alpha1 "github.com/Mellanox/spectrum-x-operator/api/v1alpha1"
	"github.com/Mellanox/spectrum-x-operator/internal/controller"
	"github.com/Mellanox/spectrum-x-operator/internal/staleflows"
	"github.com/Mellanox/spectrum-x-operator/pkg/exec"
	"github.com/Mellanox/spectrum-x-operator/pkg/filewatcher"
	"github.com/Mellanox/spectrum-x-operator/pkg/lib/netlink"

	env "github.com/caarlos0/env/v11"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(spectrumxv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

var Options struct {
	NodeName string `env:"NODE_NAME"`
}

//nolint:funlen
func main() {
	var metricsAddr string
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&secureMetrics, "metrics-secure", false,
		"If set the metrics endpoint is served securely")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if err := env.Parse(&Options); err != nil {
		setupLog.Error(err, "failed to parse service options")
		os.Exit(1)
	}

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	tlsOpts := []func(*tls.Config){}
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   metricsAddr,
			SecureServing: secureMetrics,
			TLSOpts:       tlsOpts,
		},
		Cache: cache.Options{
			// To reduce cache memory consumption, only pods on on the same node as the reconciler will be added to the cache:
			// 1) List calls using the controller Manager client will only return Pods with `spec.nodeName` set to nodeName
			// 2) Get calls for pods not in the cache will never find the pod.
			ByObject: map[client.Object]cache.ByObject{
				&corev1.Pod{}: {Field: fields.SelectorFromSet(map[string]string{"spec.nodeName": Options.NodeName})},
			},
		},
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	ovsWatcherHostConfig := make(chan event.TypedGenericEvent[struct{}])
	ovsWatcherFlowsController := make(chan event.TypedGenericEvent[struct{}])

	filewatcher.WatchFile("/var/run/openvswitch/ovs-vswitchd.pid", func() {
		ovsWatcherHostConfig <- event.TypedGenericEvent[struct{}]{}
		ovsWatcherFlowsController <- event.TypedGenericEvent[struct{}]{}
	}, nil)

	// index pods by node name, used by the FlowReconciler to watch relevant pods
	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&corev1.Pod{},
		"spec.nodeName",
		func(obj client.Object) []string {
			pod := obj.(*corev1.Pod)
			return []string{pod.Spec.NodeName}
		},
	); err != nil {
		setupLog.Error(err, "unable to index pods by node name")
		os.Exit(1)
	}

	if err = (&controller.FlowReconciler{
		NodeName:   Options.NodeName,
		Client:     mgr.GetClient(),
		Exec:       &exec.Exec{},
		Flows:      &controller.Flows{Exec: &exec.Exec{}, NetlinkLib: netlink.New()},
		OVSWatcher: ovsWatcherFlowsController,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Pod")
		os.Exit(1)
	}

	staleFlowsCleaner := &staleflows.Cleaner{
		Client:          mgr.GetClient(),
		Flows:           &controller.Flows{Exec: &exec.Exec{}, NetlinkLib: netlink.New()},
		CleanupInterval: 5 * time.Minute,
	}

	staleFlowsCleaner.StartCleanupRoutine(context.Background())

	if err := (&controller.SpectrumXRailPoolConfigIPAssignerReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "SpectrumXRailPoolConfig")
		os.Exit(1)
	}
	if err := (&controller.SpectrumXRailPoolConfigHostFlowsReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "SpectrumXRailPoolConfig")
		os.Exit(1)
	}

	//+kubebuilder:scaffold:builder

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
