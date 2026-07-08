/*
Copyright The Kubernetes Authors.

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

package performance

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	v1 "github.com/aws/karpenter-provider-aws/pkg/apis/v1"
	"github.com/aws/karpenter-provider-aws/test/pkg/environment/aws"
	"github.com/aws/karpenter-provider-aws/test/pkg/environment/common"
)

// Package-scoped test state.
//
// The OSS performance test bodies reference `env`, `nodePool`, and `nodeClass`
// as package-level vars. To keep test bodies textually close to OSS, we do the
// same here. Types are adapted for aws-provider:
//   - env: *common.Environment (aws-provider's), so the same value works with
//     both the ported OSS Report* helpers and aws-provider's own env methods.
//     The aws-specific env (with EC2/EKS/IAM clients) is held in awsEnv for
//     setup/teardown that needs AWS APIs.
//   - nodeClass: *v1.EC2NodeClass (OSS uses *unstructured.Unstructured).
//   - nodePool: *karpv1.NodePool (same as OSS).
var awsEnv *aws.Environment
var env *common.Environment
var nodePool *karpv1.NodePool
var nodeClass *v1.EC2NodeClass

func TestPerformance(t *testing.T) {
	RegisterFailHandler(Fail)
	BeforeSuite(func() {
		awsEnv = aws.NewEnvironment(t)
		env = awsEnv.Environment
		SetDefaultEventuallyTimeout(time.Hour)
	})
	AfterSuite(func() {
		awsEnv.Stop()
	})
	RunSpecs(t, "Performance")
}

var _ = BeforeEach(func() {
	awsEnv.BeforeEach()
	nodeClass = awsEnv.DefaultEC2NodeClass()
	nodePool = awsEnv.DefaultNodePool(nodeClass)
	// Match OSS setup: unbounded scale, aggressive consolidation, full disruption budget.
	nodePool.Spec.Limits = karpv1.Limits{}
	nodePool.Spec.Disruption.ConsolidationPolicy = karpv1.ConsolidationPolicyWhenEmptyOrUnderutilized
	nodePool.Spec.Disruption.ConsolidateAfter = karpv1.MustParseNillableDuration("30s")
	nodePool.Spec.Disruption.Budgets = []karpv1.Budget{{Nodes: "100%"}}
})

var _ = AfterEach(func() {
	awsEnv.Cleanup()
})
var _ = AfterEach(func() {
	awsEnv.AfterEach()
})
