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
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

const (
	defaultVersion  = "latest"
	releaseBaseURL  = "https://github.com/gfontana/kube-copilot-agent/releases"
	manifestURLTmpl = releaseBaseURL + "/download/%s/install.yaml"
	latestURL       = releaseBaseURL + "/latest/download/install.yaml"
)

var installVersion string

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the KubeCopilot operator into the cluster",
	Long:  `Apply the KubeCopilot operator manifests (CRDs, RBAC, Deployment) from a GitHub release.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		url := latestURL
		if installVersion != "" && installVersion != defaultVersion {
			url = fmt.Sprintf(manifestURLTmpl, installVersion)
		}
		_, _ = fmt.Fprintf(os.Stdout, "Installing KubeCopilot operator from %s\n", url)
		return runKubectl("apply", "-f", url)
	},
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall the KubeCopilot operator from the cluster",
	Long:  `Remove the KubeCopilot operator manifests (CRDs, RBAC, Deployment) from the cluster.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		url := latestURL
		if installVersion != "" && installVersion != defaultVersion {
			url = fmt.Sprintf(manifestURLTmpl, installVersion)
		}
		_, _ = fmt.Fprintf(os.Stdout, "Uninstalling KubeCopilot operator using %s\n", url)
		return runKubectl("delete", "--ignore-not-found", "-f", url)
	},
}

func init() {
	installCmd.Flags().StringVar(&installVersion, "version", defaultVersion, "operator release version (e.g. v1.0.0)")
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(uninstallCmd)
}

func runKubectl(args ...string) error {
	kubectlArgs := args
	if kubeconfig != "" {
		kubectlArgs = append([]string{"--kubeconfig", kubeconfig}, kubectlArgs...)
	}
	if kubeCtx != "" {
		kubectlArgs = append([]string{"--context", kubeCtx}, kubectlArgs...)
	}
	c := exec.Command("kubectl", kubectlArgs...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
