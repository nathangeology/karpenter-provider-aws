/*
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

package compatibility_test

import (
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	v1 "github.com/aws/karpenter-provider-aws/pkg/apis/v1"
	"github.com/aws/karpenter-provider-aws/pkg/providers/instancetype/compatibility"
)

// BenchmarkIsCompatibleWithNodeClass measures the compatibility fanout that
// the scheduling loop performs on every simulation: for each NodePool,
// evaluate compatibility against ~all EC2 instance types. Primary purpose:
// A/B v1.13.0 vs v1.13.0-revert-9178 to measure the cost of d3a72b03 (PR
// #9178) which changed path (4) in networkInterfaceCheck.compatibleCheck.
//
// Two sub-scenarios are benchmarked:
//
//  1. "prod-shape": NodeClass.NetworkInterfaces empty (the default for the
//     vast majority of NodePools). This is what the OSS scheduling loop
//     actually hits in production — the compatibility.go early return at
//     line 85 (`if c.networkInterfaces == nil { return true }`) short-
//     circuits before path (4) ever runs. Any delta here is noise or an
//     effect on other checks (AMI, nested virt, connection tracking).
//
//  2. "with-nis": NodeClass.NetworkInterfaces populated so path (4) fires
//     for every instance. Reveals the micro-cost of d3a72b03 on the path
//     the commit actually modified.
//
// If prod-shape shows no delta and with-nis shows a delta, the aws-provider
// tree is NOT implicated at this layer — because ResolveNetworkInterfaces
// (pkg/providers/amifamily/networkinterface.go) currently populates only
// NetworkCardIndex/DeviceIndex/InterfaceType, leaving SecondaryIPCount and
// SecondaryIPPrefixCount nil. That means path (4)'s new arithmetic never
// executes in production even when networkInterfaces is set.
func BenchmarkIsCompatibleWithNodeClass(b *testing.B) {
	infos := benchBuildInstanceTypeInfos(800)

	for _, scenario := range []struct {
		name    string
		builder func(int) []*benchMockNodeClass
	}{
		{"prod-shape", benchBuildNodeClassesProdShape},
		{"with-nis", benchBuildNodeClassesWithNIs},
	} {
		for _, n := range []int{1, 5, 20} {
			ncs := scenario.builder(n)
			b.Run(fmt.Sprintf("%s/nodepools=%d", scenario.name, n), func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					for _, nc := range ncs {
						for j := range infos {
							_ = compatibility.IsCompatibleWithNodeClass(infos[j], nc, nil)
						}
					}
				}
			})
		}
	}
}

// benchBuildInstanceTypeInfos synthesises n InstanceTypeInfo values that all
// pass AMI + ENA + NetworkCards checks so the benchmark reaches path (4) —
// the code d3a72b03 modified. Variation in NumberOfCards, MaximumNetworkInterfaces,
// and Ipv4AddressesPerInterface reflects the real EC2 catalog spread.
func benchBuildInstanceTypeInfos(n int) []ec2types.InstanceTypeInfo {
	infos := make([]ec2types.InstanceTypeInfo, n)
	for i := 0; i < n; i++ {
		numCards := int32(1 + (i % 4))
		maxENIs := int32(2 + (i % 15))
		ipsPerENI := int32(4 + (i % 47))
		cards := make([]ec2types.NetworkCardInfo, numCards)
		for c := int32(0); c < numCards; c++ {
			cards[c] = ec2types.NetworkCardInfo{
				NetworkCardIndex:         aws.Int32(c),
				MaximumNetworkInterfaces: aws.Int32(maxENIs),
			}
		}
		infos[i] = ec2types.InstanceTypeInfo{
			InstanceType: ec2types.InstanceType(fmt.Sprintf("bench.%d", i)),
			ProcessorInfo: &ec2types.ProcessorInfo{
				SupportedArchitectures: []ec2types.ArchitectureType{ec2types.ArchitectureTypeX8664},
			},
			VCpuInfo: &ec2types.VCpuInfo{
				DefaultVCpus: aws.Int32(2),
			},
			MemoryInfo: &ec2types.MemoryInfo{
				SizeInMiB: aws.Int64(8192),
			},
			NetworkInfo: &ec2types.NetworkInfo{
				EnaSupport:                ec2types.EnaSupportSupported,
				NetworkCards:              cards,
				Ipv4AddressesPerInterface: aws.Int32(ipsPerENI),
			},
			Hypervisor: ec2types.InstanceTypeHypervisorNitro,
		}
	}
	return infos
}

// benchBuildNodeClassesProdShape returns NodeClasses with NetworkInterfaces
// == nil, which is the default in production. compatibility.go short-
// circuits path (4) via the c.networkInterfaces == nil check.
func benchBuildNodeClassesProdShape(n int) []*benchMockNodeClass {
	ncs := make([]*benchMockNodeClass, n)
	for i := 0; i < n; i++ {
		ncs[i] = &benchMockNodeClass{
			amiFamily:         v1.AMIFamilyCustom,
			networkInterfaces: nil,
		}
	}
	return ncs
}

// benchBuildNodeClassesWithNIs returns NodeClasses with NetworkInterfaces
// populated so path (4) executes for every instance. NB: v1.NetworkInterface
// does not have SecondaryIPCount / SecondaryIPPrefixCount fields — those
// live on amifamily.ResolvedNetworkInterface but are never populated by
// ResolveNetworkInterfaces. So even here, secondaryIPsConfigured == 0
// and the "if > 0" gate d3a72b03 restructured still short-circuits.
// The benchmark still measures the max() call and gate check overhead.
func benchBuildNodeClassesWithNIs(n int) []*benchMockNodeClass {
	ncs := make([]*benchMockNodeClass, n)
	for i := 0; i < n; i++ {
		ncs[i] = &benchMockNodeClass{
			amiFamily: v1.AMIFamilyCustom,
			networkInterfaces: []*v1.NetworkInterface{
				{NetworkCardIndex: 0, DeviceIndex: 0, InterfaceType: v1.InterfaceTypeInterface},
				{NetworkCardIndex: 0, DeviceIndex: 1, InterfaceType: v1.InterfaceTypeInterface},
			},
		}
	}
	return ncs
}

type benchMockNodeClass struct {
	amiFamily          string
	networkInterfaces  []*v1.NetworkInterface
	cpuOptions         *v1.CPUOptions
	connectionTracking *v1.ConnectionTracking
}

func (m *benchMockNodeClass) AMIFamily() string                          { return m.amiFamily }
func (m *benchMockNodeClass) NetworkInterfaces() []*v1.NetworkInterface  { return m.networkInterfaces }
func (m *benchMockNodeClass) CPUOptions() *v1.CPUOptions                 { return m.cpuOptions }
func (m *benchMockNodeClass) ConnectionTracking() *v1.ConnectionTracking { return m.connectionTracking }
