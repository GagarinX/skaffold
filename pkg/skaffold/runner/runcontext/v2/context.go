/*
Copyright 2019 The Skaffold Authors

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

package v2

import (
	"context"
	"fmt"
	"os"

	"github.com/google/uuid"

	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/config"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/constants"
	kubectx "github.com/GoogleContainerTools/skaffold/pkg/skaffold/kubernetes/context"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/output/log"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/schema/latest"
	schemaUtil "github.com/GoogleContainerTools/skaffold/pkg/skaffold/schema/util"
)

type RunContext struct {
	Opts               config.SkaffoldOptions
	Pipelines          Pipelines
	KubeContext        string
	Namespaces         []string
	WorkingDir         string
	InsecureRegistries map[string]bool
	Cluster            config.Cluster
	RunID              string
}

// Pipelines encapsulates multiple config pipelines
type Pipelines struct {
	pipelines            []latest.Pipeline
	pipelinesByImageName map[string]latest.Pipeline
}

// All returns all config pipelines.
func (ps Pipelines) All() []latest.Pipeline {
	return ps.pipelines
}

// Head returns the first `latest.Pipeline`.
func (ps Pipelines) Head() latest.Pipeline {
	return ps.pipelines[0] // there always exists atleast one pipeline.
}

// Select returns the first `latest.Pipeline` that matches the given artifact `imageName`.
func (ps Pipelines) Select(imageName string) (latest.Pipeline, bool) {
	p, found := ps.pipelinesByImageName[imageName]
	return p, found
}

// IsMultiPipeline returns true if there are more than one constituent skaffold pipelines.
func (ps Pipelines) IsMultiPipeline() bool {
	return len(ps.pipelines) > 1
}

func (ps Pipelines) PortForwardResources() []*latest.PortForwardResource {
	var pf []*latest.PortForwardResource
	for _, p := range ps.pipelines {
		pf = append(pf, p.PortForward...)
	}
	return pf
}

func (ps Pipelines) Artifacts() []*latest.Artifact {
	var artifacts []*latest.Artifact
	for _, p := range ps.pipelines {
		artifacts = append(artifacts, p.Build.Artifacts...)
	}
	return artifacts
}

// TODO: Clean up code duplication
func (ps Pipelines) DeployConfigs() []latest.DeployConfig {
	var cfgs []latest.DeployConfig
	for _, p := range ps.pipelines {
		cfgs = append(cfgs, p.Deploy)
	}
	return cfgs
}

func (ps Pipelines) Deployers() []latest.DeployConfig {
	var deployers []latest.DeployConfig
	for _, p := range ps.pipelines {
		deployers = append(deployers, p.Deploy)
	}
	return deployers
}

func (ps Pipelines) Renderers() []latest.RenderConfig {
	var rcs []latest.RenderConfig
	for _, p := range ps.pipelines {
		rcs = append(rcs, p.Render)
	}
	return rcs
}

func (ps Pipelines) TestCases() []*latest.TestCase {
	var tests []*latest.TestCase
	for _, p := range ps.pipelines {
		tests = append(tests, p.Test...)
	}
	return tests
}

// TransformAllowList returns combined allowlist from pipelines
func (ps Pipelines) TransformAllowList() []latest.ResourceFilter {
	var allowlist []latest.ResourceFilter
	for _, p := range ps.pipelines {
		if p.ResourceSelector.Allow != nil {
			allowlist = append(allowlist, p.ResourceSelector.Allow...)
		}
	}
	return allowlist
}

// TransformDenyList returns combined denylist from pipelines
func (ps Pipelines) TransformDenyList() []latest.ResourceFilter {
	var denylist []latest.ResourceFilter
	for _, p := range ps.pipelines {
		if p.ResourceSelector.Deny != nil {
			denylist = append(denylist, p.ResourceSelector.Deny...)
		}
	}
	return denylist
}

func (ps Pipelines) StatusCheckDeadlineSeconds() int {
	c := 0
	// set the group status check deadline to maximum of any individually specified value
	for _, p := range ps.pipelines {
		if p.Deploy.StatusCheckDeadlineSeconds > c {
			c = p.Deploy.StatusCheckDeadlineSeconds
		}
	}
	return c
}
func NewPipelines(pipelines []latest.Pipeline) Pipelines {
	m := make(map[string]latest.Pipeline)
	for _, p := range pipelines {
		for _, a := range p.Build.Artifacts {
			m[a.ImageName] = p
		}
	}
	return Pipelines{pipelines: pipelines, pipelinesByImageName: m}
}

func (rc *RunContext) PipelineForImage(imageName string) (latest.Pipeline, bool) {
	return rc.Pipelines.Select(imageName)
}

func (rc *RunContext) PortForwardResources() []*latest.PortForwardResource {
	return rc.Pipelines.PortForwardResources()
}

func (rc *RunContext) Artifacts() []*latest.Artifact { return rc.Pipelines.Artifacts() }

func (rc *RunContext) DeployConfigs() []latest.DeployConfig { return rc.Pipelines.DeployConfigs() }

func (rc *RunContext) Deployers() []latest.DeployConfig { return rc.Pipelines.Deployers() }

func (rc *RunContext) Renderers() []latest.RenderConfig { return rc.Pipelines.Renderers() }

func (rc *RunContext) TestCases() []*latest.TestCase { return rc.Pipelines.TestCases() }

func (rc *RunContext) StatusCheckDeadlineSeconds() int {
	return rc.Pipelines.StatusCheckDeadlineSeconds()
}

func (rc *RunContext) SkipTests() bool {
	return rc.Opts.SkipTests
}

func (rc *RunContext) IsTestPhaseActive() bool {
	return !rc.SkipTests() && len(rc.TestCases()) != 0
}

func (rc *RunContext) TransformAllowList() []latest.ResourceFilter {
	return rc.Pipelines.TransformAllowList()
}

func (rc *RunContext) TransformDenyList() []latest.ResourceFilter {
	return rc.Pipelines.TransformDenyList()
}

// AddSkaffoldLabels tells the Runner whether to add skaffold-specific labels.
// We only ever skip adding labels during a `skaffold render`.
func (rc *RunContext) AddSkaffoldLabels() bool {
	return rc.Opts.Mode() != config.RunModes.Render
}

func (rc *RunContext) UsingLegacyHelmDeploy() bool {
	for _, config := range rc.DeployConfigs() {
		if config.LegacyHelmDeploy != nil {
			return true
		}
	}

	return false
}

func (rc *RunContext) DefaultPipeline() latest.Pipeline              { return rc.Pipelines.Head() }
func (rc *RunContext) GetKubeContext() string                        { return rc.KubeContext }
func (rc *RunContext) GetNamespaces() []string                       { return rc.Namespaces }
func (rc *RunContext) GetPipelines() []latest.Pipeline               { return rc.Pipelines.All() }
func (rc *RunContext) GetInsecureRegistries() map[string]bool        { return rc.InsecureRegistries }
func (rc *RunContext) GetWorkingDir() string                         { return rc.WorkingDir }
func (rc *RunContext) GetCluster() config.Cluster                    { return rc.Cluster }
func (rc *RunContext) GetNamespace() string                          { return rc.Opts.Namespace }
func (rc *RunContext) AutoBuild() bool                               { return rc.Opts.AutoBuild }
func (rc *RunContext) AutoDeploy() bool                              { return rc.Opts.AutoDeploy }
func (rc *RunContext) AutoSync() bool                                { return rc.Opts.AutoSync }
func (rc *RunContext) ContainerDebugging() bool                      { return rc.Opts.ContainerDebugging }
func (rc *RunContext) CacheArtifacts() bool                          { return rc.Opts.CacheArtifacts }
func (rc *RunContext) CacheFile() string                             { return rc.Opts.CacheFile }
func (rc *RunContext) ConfigurationFile() string                     { return rc.Opts.ConfigurationFile }
func (rc *RunContext) CustomLabels() []string                        { return rc.Opts.CustomLabels }
func (rc *RunContext) CustomTag() string                             { return rc.Opts.CustomTag }
func (rc *RunContext) DefaultRepo() *string                          { return rc.Cluster.DefaultRepo.Value() }
func (rc *RunContext) MultiLevelRepo() *bool                         { return rc.Opts.MultiLevelRepo }
func (rc *RunContext) Mode() config.RunMode                          { return rc.Opts.Mode() }
func (rc *RunContext) DryRun() bool                                  { return rc.Opts.DryRun }
func (rc *RunContext) ForceDeploy() bool                             { return rc.Opts.Force }
func (rc *RunContext) GetKubeConfig() string                         { return rc.Opts.KubeConfig }
func (rc *RunContext) GetKubeNamespace() string                      { return rc.Opts.Namespace }
func (rc *RunContext) GlobalConfig() string                          { return rc.Opts.GlobalConfig }
func (rc *RunContext) HydratedManifests() []string                   { return rc.Opts.HydratedManifests }
func (rc *RunContext) LoadImages() bool                              { return rc.Cluster.LoadImages }
func (rc *RunContext) ForceLoadImages() bool                         { return rc.Opts.ForceLoadImages }
func (rc *RunContext) MinikubeProfile() string                       { return rc.Opts.MinikubeProfile }
func (rc *RunContext) Muted() config.Muted                           { return rc.Opts.Muted }
func (rc *RunContext) NoPruneChildren() bool                         { return rc.Opts.NoPruneChildren }
func (rc *RunContext) Notification() bool                            { return rc.Opts.Notification }
func (rc *RunContext) PortForward() bool                             { return rc.Opts.PortForward.Enabled() }
func (rc *RunContext) PortForwardOptions() config.PortForwardOptions { return rc.Opts.PortForward }
func (rc *RunContext) Prune() bool                                   { return rc.Opts.Prune() }
func (rc *RunContext) RenderOnly() bool                              { return rc.Opts.RenderOnly }
func (rc *RunContext) RenderOutput() string                          { return rc.Opts.RenderOutput }
func (rc *RunContext) SkipRender() bool                              { return rc.Opts.SkipRender }
func (rc *RunContext) StatusCheck() *bool                            { return rc.Opts.StatusCheck.Value() }
func (rc *RunContext) IterativeStatusCheck() bool                    { return rc.Opts.IterativeStatusCheck }
func (rc *RunContext) FastFailStatusCheck() bool                     { return rc.Opts.FastFailStatusCheck }
func (rc *RunContext) Tail() bool                                    { return rc.Opts.Tail }
func (rc *RunContext) Trigger() string                               { return rc.Opts.Trigger }
func (rc *RunContext) WaitForDeletions() config.WaitForDeletions     { return rc.Opts.WaitForDeletions }
func (rc *RunContext) WatchPollInterval() int                        { return rc.Opts.WatchPollInterval }
func (rc *RunContext) BuildConcurrency() int                         { return rc.Opts.BuildConcurrency }
func (rc *RunContext) IsMultiConfig() bool                           { return rc.Pipelines.IsMultiPipeline() }
func (rc *RunContext) IsDefaultKubeContext() bool                    { return rc.Opts.KubeContext == "" }
func (rc *RunContext) GetRunID() string                              { return rc.RunID }
func (rc *RunContext) RPCPort() *int                                 { return rc.Opts.RPCPort.Value() }
func (rc *RunContext) RPCHTTPPort() *int                             { return rc.Opts.RPCHTTPPort.Value() }
func (rc *RunContext) PushImages() config.BoolOrUndefined            { return rc.Opts.PushImages }
func (rc *RunContext) TransformRulesFile() string                    { return rc.Opts.TransformRulesFile }
func (rc *RunContext) VerifyDockerNetwork() string                   { return rc.Opts.VerifyDockerNetwork }
func (rc *RunContext) JSONParseConfig() latest.JSONParseConfig {
	return rc.DefaultPipeline().Deploy.Logs.JSONParse
}

// GetRenderConfig returns the top tier RenderConfig.
// TODO: design how to support multi-module.
func (rc *RunContext) GetRenderConfig() *latest.RenderConfig {
	p := rc.GetPipelines()
	if len(p) > 0 {
		return &p[0].Render
	}
	return &latest.RenderConfig{}
}

func (rc *RunContext) DigestSource() string {
	if rc.Opts.DigestSource != "" {
		return rc.Opts.DigestSource
	}
	if rc.Cluster.Local {
		return constants.TagDigestSource
	}
	return constants.RemoteDigestSource
}

func GetRunContext(ctx context.Context, opts config.SkaffoldOptions, configs []schemaUtil.VersionedConfig) (*RunContext, error) {
	var pipelines []latest.Pipeline
	for _, cfg := range configs {
		if cfg != nil {
			pipelines = append(pipelines, cfg.(*latest.SkaffoldConfig).Pipeline)
		}
	}
	kubeConfig, err := kubectx.CurrentConfig()
	if err != nil {
		return nil, fmt.Errorf("getting current cluster context: %w", err)
	}
	kubeContext := kubeConfig.CurrentContext
	log.Entry(context.TODO()).Infof("Using kubectl context: %s", kubeContext)

	// TODO(dgageot): this should be the folder containing skaffold.yaml. Should also be moved elsewhere.
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("finding current directory: %w", err)
	}

	// combine all provided lists of insecure registries into a map
	cfgRegistries, err := config.GetInsecureRegistries(opts.GlobalConfig)
	if err != nil {
		log.Entry(context.TODO()).Warn("error retrieving insecure registries from global config: push/pull issues may exist...")
	}
	var regList []string
	regList = append(regList, opts.InsecureRegistries...)
	for _, cfg := range pipelines {
		regList = append(regList, cfg.Build.InsecureRegistries...)
	}
	regList = append(regList, cfgRegistries...)
	insecureRegistries := make(map[string]bool, len(regList))
	for _, r := range regList {
		insecureRegistries[r] = true
	}
	ps := NewPipelines(pipelines)

	// TODO(https://github.com/GoogleContainerTools/skaffold/issues/3668):
	// remove minikubeProfile from here and instead detect it by matching the
	// kubecontext API Server to minikube profiles
	cluster, err := config.GetCluster(ctx, config.GetClusterOpts{
		ConfigFile:      opts.GlobalConfig,
		DefaultRepo:     opts.DefaultRepo,
		MinikubeProfile: opts.MinikubeProfile,
		DetectMinikube:  opts.DetectMinikube,
	})
	if err != nil {
		return nil, fmt.Errorf("getting cluster: %w", err)
	}

	runID := uuid.New().String()

	return &RunContext{
		Opts:               opts,
		Pipelines:          ps,
		WorkingDir:         cwd,
		KubeContext:        kubeContext,
		InsecureRegistries: insecureRegistries,
		Cluster:            cluster,
		RunID:              runID,
	}, nil
}
