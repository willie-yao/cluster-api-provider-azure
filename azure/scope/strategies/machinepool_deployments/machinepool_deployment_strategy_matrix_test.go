/*
Copyright 2026 The Kubernetes Authors.

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

package machinepool

import (
	"fmt"
	"sort"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	infrav1exp "sigs.k8s.io/cluster-api-provider-azure/exp/api/v1beta1"
	"sigs.k8s.io/cluster-api-provider-azure/feature"
)

type selectorMatrixCase struct {
	readyOld          int
	readyLatest       int
	unreadyOld        int
	unreadyLatest     int
	unknown           int
	desired           int
	maxUnavailable    int
	rolloutInProgress bool
	deletePolicy      infrav1exp.AzureMachinePoolDeletePolicyType
}

type matrixDimension struct {
	size int
	set  func(*selectorMatrixCase, int)
}

// TestMachinePoolRollingUpdateStrategy_SelectMachinesToDeleteMatrix covers normal observed machines.
// Focused tests cover failed, deleting, annotated, and randomly ordered machines.
func TestMachinePoolRollingUpdateStrategy_SelectMachinesToDeleteMatrix(t *testing.T) {
	baseTime := time.Now().Add(-24 * time.Hour).Truncate(time.Microsecond)
	matrixCases := selectorMatrixCases()
	if got, want := len(matrixCases), 6480; got != want {
		t.Fatalf("expected %d selector matrix cases, got %d", want, got)
	}

	for _, skipModel := range []bool{false, true} {
		t.Run(fmt.Sprintf("skipModel=%t", skipModel), func(t *testing.T) {
			featuregatetesting.SetFeatureGateDuringTest(t, feature.Gates, feature.SkipMachinePoolModelReconciliation, skipModel)
			g := NewWithT(t)

			for _, testCase := range matrixCases {
				scenario := testCase.String()
				machines, readyMachines, unreadyMachines := makeMatrixMachines(
					baseTime,
					testCase.readyOld,
					testCase.readyLatest,
					testCase.unreadyOld,
					testCase.unreadyLatest,
					testCase.unknown,
				)
				orderMatrixMachines(readyMachines, testCase.deletePolicy)
				orderMatrixMachines(unreadyMachines, testCase.deletePolicy)

				rollingUpdate := infrav1exp.MachineRollingUpdateDeployment{DeletePolicy: testCase.deletePolicy}
				if testCase.maxUnavailable > 0 {
					value := intstr.FromInt(testCase.maxUnavailable)
					rollingUpdate.MaxUnavailable = &value
				}

				strategy := makeRollingUpdateStrategy(rollingUpdate)
				got, err := strategy.SelectMachinesToDelete(t.Context(), int32(testCase.desired), machines, testCase.rolloutInProgress)
				g.Expect(err).NotTo(HaveOccurred(), scenario)

				want := expectedMatrixSelection(
					readyMachines,
					unreadyMachines,
					testCase.desired,
					testCase.maxUnavailable,
					testCase.rolloutInProgress,
					skipModel,
				)
				g.Expect(matrixMachineNames(got)).To(Equal(matrixMachineNames(want)), scenario)
			}
		})
	}
}

func selectorMatrixCases() []selectorMatrixCase {
	policies := []infrav1exp.AzureMachinePoolDeletePolicyType{
		infrav1exp.OldestDeletePolicyType,
		infrav1exp.NewestDeletePolicyType,
	}
	dimensions := []matrixDimension{
		{size: 3, set: func(testCase *selectorMatrixCase, value int) { testCase.readyOld = value }},
		{size: 3, set: func(testCase *selectorMatrixCase, value int) { testCase.readyLatest = value }},
		{size: 3, set: func(testCase *selectorMatrixCase, value int) { testCase.unreadyOld = value }},
		{size: 3, set: func(testCase *selectorMatrixCase, value int) { testCase.unreadyLatest = value }},
		{size: 2, set: func(testCase *selectorMatrixCase, value int) { testCase.unknown = value }},
		{size: 5, set: func(testCase *selectorMatrixCase, value int) { testCase.desired = value }},
		{size: 2, set: func(testCase *selectorMatrixCase, value int) { testCase.maxUnavailable = value }},
		{size: 2, set: func(testCase *selectorMatrixCase, value int) { testCase.rolloutInProgress = value == 1 }},
		{size: len(policies), set: func(testCase *selectorMatrixCase, value int) { testCase.deletePolicy = policies[value] }},
	}

	cases := []selectorMatrixCase{{}}
	for _, dimension := range dimensions {
		next := make([]selectorMatrixCase, 0, len(cases)*dimension.size)
		for _, base := range cases {
			for value := range dimension.size {
				testCase := base
				dimension.set(&testCase, value)
				next = append(next, testCase)
			}
		}
		cases = next
	}
	return cases
}

func (testCase selectorMatrixCase) String() string {
	return fmt.Sprintf(
		"readyOld=%d readyLatest=%d unreadyOld=%d unreadyLatest=%d unknown=%d desired=%d maxUnavailable=%d rollout=%t policy=%s",
		testCase.readyOld,
		testCase.readyLatest,
		testCase.unreadyOld,
		testCase.unreadyLatest,
		testCase.unknown,
		testCase.desired,
		testCase.maxUnavailable,
		testCase.rolloutInProgress,
		testCase.deletePolicy,
	)
}

func makeMatrixMachines(baseTime time.Time, readyOld, readyLatest, unreadyOld, unreadyLatest, unknown int) (map[string]infrav1exp.AzureMachinePoolMachine, []infrav1exp.AzureMachinePoolMachine, []infrav1exp.AzureMachinePoolMachine) {
	machines := map[string]infrav1exp.AzureMachinePoolMachine{}
	readyMachines := make([]infrav1exp.AzureMachinePoolMachine, 0, readyOld+readyLatest)
	unreadyMachines := make([]infrav1exp.AzureMachinePoolMachine, 0, unreadyOld+unreadyLatest)

	add := func(prefix string, count, offset int, ready, latest bool) {
		for i := range count {
			name := fmt.Sprintf("%s-%d", prefix, i)
			machine := makeAMPM(ampmOptions{
				Ready:             ready,
				LatestModel:       latest,
				ProvisioningState: infrav1.Succeeded,
				CreationTime:      metav1.NewTime(baseTime.Add(time.Duration(offset+4*i) * time.Minute)),
			})
			machine.Name = name
			machines[name] = machine
			if ready {
				readyMachines = append(readyMachines, machine)
			} else {
				unreadyMachines = append(unreadyMachines, machine)
			}
		}
	}

	add("ready-old", readyOld, 0, true, false)
	add("ready-latest", readyLatest, 1, true, true)
	add("unready-old", unreadyOld, 2, false, false)
	add("unready-latest", unreadyLatest, 3, false, true)

	for i := range unknown {
		name := fmt.Sprintf("unknown-%d", i)
		machine := makeAMPMWithEmptyStatus()
		machine.Name = name
		machines[name] = machine
	}

	return machines, readyMachines, unreadyMachines
}

func orderMatrixMachines(machines []infrav1exp.AzureMachinePoolMachine, policy infrav1exp.AzureMachinePoolDeletePolicyType) {
	sort.Slice(machines, func(i, j int) bool {
		if policy == infrav1exp.NewestDeletePolicyType {
			return machines[i].CreationTimestamp.After(machines[j].CreationTimestamp.Time)
		}
		return machines[i].CreationTimestamp.Before(&machines[j].CreationTimestamp)
	})
}

func expectedMatrixSelection(readyMachines, unreadyMachines []infrav1exp.AzureMachinePoolMachine, desired, maxUnavailable int, rolloutInProgress, skipModel bool) []infrav1exp.AzureMachinePoolMachine {
	readyOldCount := countMatrixMachines(readyMachines, false)
	unreadyLatestCount := countMatrixMachines(unreadyMachines, true)

	protectedUnreadyCount := 0
	if rolloutInProgress {
		protectedUnreadyCount = min(readyOldCount, unreadyLatestCount)
	}

	overProvisionCount := len(readyMachines) + len(unreadyMachines) - protectedUnreadyCount - desired
	if overProvisionCount > 0 {
		candidates := make([]infrav1exp.AzureMachinePoolMachine, 0, len(readyMachines)+len(unreadyMachines))
		unreadyLatestDeleteCount := unreadyLatestCount - protectedUnreadyCount

		if skipModel {
			for _, machine := range unreadyMachines {
				if machine.Status.LatestModelApplied {
					if unreadyLatestDeleteCount <= 0 {
						continue
					}
					unreadyLatestDeleteCount--
				}
				candidates = append(candidates, machine)
			}
			candidates = append(candidates, readyMachines...)
		} else {
			for _, machine := range unreadyMachines {
				if !machine.Status.LatestModelApplied {
					candidates = append(candidates, machine)
				}
			}
			for _, machine := range unreadyMachines {
				if machine.Status.LatestModelApplied && unreadyLatestDeleteCount > 0 {
					candidates = append(candidates, machine)
					unreadyLatestDeleteCount--
				}
			}
			for _, machine := range readyMachines {
				if !machine.Status.LatestModelApplied {
					candidates = append(candidates, machine)
				}
			}
			for _, machine := range readyMachines {
				if machine.Status.LatestModelApplied {
					candidates = append(candidates, machine)
				}
			}
		}

		return firstMatrixMachines(candidates, overProvisionCount)
	}

	if len(readyMachines) < desired || skipModel || readyOldCount == 0 {
		return []infrav1exp.AzureMachinePoolMachine{}
	}

	disruptionBudget := len(readyMachines) - desired + maxUnavailable
	if maxUnavailable > desired {
		disruptionBudget = desired
	}
	if disruptionBudget <= 0 {
		return []infrav1exp.AzureMachinePoolMachine{}
	}

	readyOldMachines := make([]infrav1exp.AzureMachinePoolMachine, 0, readyOldCount)
	for _, machine := range readyMachines {
		if !machine.Status.LatestModelApplied {
			readyOldMachines = append(readyOldMachines, machine)
		}
	}
	return firstMatrixMachines(readyOldMachines, disruptionBudget)
}

func countMatrixMachines(machines []infrav1exp.AzureMachinePoolMachine, latest bool) int {
	count := 0
	for _, machine := range machines {
		if machine.Status.LatestModelApplied == latest {
			count++
		}
	}
	return count
}

func firstMatrixMachines(machines []infrav1exp.AzureMachinePoolMachine, count int) []infrav1exp.AzureMachinePoolMachine {
	if count > len(machines) {
		count = len(machines)
	}
	return machines[:count]
}

func matrixMachineNames(machines []infrav1exp.AzureMachinePoolMachine) []string {
	names := make([]string, len(machines))
	for i := range machines {
		names[i] = machines[i].Name
	}
	return names
}
