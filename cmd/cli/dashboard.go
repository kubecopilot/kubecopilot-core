/*
Copyright 2026.

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
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var dashboardPort int

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Port-forward the KubeCopilot Web UI to localhost",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := getRestConfig()
		if err != nil {
			return fmt.Errorf("failed to get kubeconfig: %w", err)
		}
		c, err := getClient()
		if err != nil {
			return fmt.Errorf("failed to create client: %w", err)
		}

		// Find the web-ui service.
		var svcList corev1.ServiceList
		if err := c.List(context.Background(), &svcList, client.InNamespace(namespace)); err != nil {
			return fmt.Errorf("failed to list services: %w", err)
		}
		var targetSvc *corev1.Service
		for i := range svcList.Items {
			s := &svcList.Items[i]
			if s.Name == "kube-copilot-agent-ui" || s.Name == "web-ui" {
				targetSvc = s
				break
			}
		}
		if targetSvc == nil {
			return fmt.Errorf("web-ui service not found in namespace %q", namespace)
		}

		// Determine target port from the service.
		var svcPort int32 = 3000
		if len(targetSvc.Spec.Ports) > 0 {
			svcPort = targetSvc.Spec.Ports[0].TargetPort.IntVal
			if svcPort == 0 {
				svcPort = targetSvc.Spec.Ports[0].Port
			}
		}

		// Find a pod backing the service.
		var podList corev1.PodList
		if err := c.List(context.Background(), &podList,
			client.InNamespace(namespace),
			client.MatchingLabels(targetSvc.Spec.Selector)); err != nil {
			return fmt.Errorf("failed to list pods: %w", err)
		}
		if len(podList.Items) == 0 {
			return fmt.Errorf("no pods found for service %q", targetSvc.Name)
		}
		podName := podList.Items[0].Name

		// Set up port-forward.
		clientset, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			return fmt.Errorf("failed to create clientset: %w", err)
		}
		req := clientset.CoreV1().RESTClient().Post().
			Resource("pods").
			Namespace(namespace).
			Name(podName).
			SubResource("portforward")

		transport, upgrader, err := spdy.RoundTripperFor(cfg)
		if err != nil {
			return fmt.Errorf("failed to create round tripper: %w", err)
		}
		dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, req.URL())

		stopChan := make(chan struct{}, 1)
		readyChan := make(chan struct{})
		ports := []string{fmt.Sprintf("%d:%d", dashboardPort, svcPort)}

		pf, err := portforward.New(dialer, ports, stopChan, readyChan, os.Stdout, os.Stderr)
		if err != nil {
			return fmt.Errorf("failed to create port-forward: %w", err)
		}

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-sigChan
			close(stopChan)
		}()

		_, _ = fmt.Fprintf(os.Stdout, "Forwarding %s/%s → http://localhost:%d\n", namespace, podName, dashboardPort)
		return pf.ForwardPorts()
	},
}

func init() {
	dashboardCmd.Flags().IntVar(&dashboardPort, "port", 3000, "local port to forward to")
	rootCmd.AddCommand(dashboardCmd)
}
