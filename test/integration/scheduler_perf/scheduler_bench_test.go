/*
Copyright 2015 The Kubernetes Authors.

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

package benchmark

import (
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/kubernetes/test/integration/framework"
	testutils "k8s.io/kubernetes/test/utils"

	"k8s.io/klog"
)

var (
	defaultNodeStrategy = &testutils.TrivialNodePrepareStrategy{}
)

// BenchmarkScheduling benchmarks the scheduling rate when the cluster has
// various quantities of nodes and scheduled pods.
func BenchmarkScheduling(b *testing.B) {
	tests := []struct{ nodes, existingPods, minPods int }{
		{nodes: 100, existingPods: 0, minPods: 100},
		{nodes: 100, existingPods: 1000, minPods: 100},
		{nodes: 1000, existingPods: 0, minPods: 100},
		{nodes: 1000, existingPods: 1000, minPods: 100},
		{nodes: 5000, existingPods: 1000, minPods: 1000},
	}
	setupStrategy := testutils.NewSimpleWithControllerCreatePodStrategy("rc1")
	testStrategy := testutils.NewSimpleWithControllerCreatePodStrategy("rc2")
	for _, test := range tests {
		name := fmt.Sprintf("%vNodes/%vPods", test.nodes, test.existingPods)
		b.Run(name, func(b *testing.B) {
			benchmarkScheduling(test.nodes, test.existingPods, test.minPods, defaultNodeStrategy, setupStrategy, testStrategy, b)
		})
	}
}

// benchmarkScheduling benchmarks scheduling rate with specific number of nodes
// and specific number of pods already scheduled.
// This will schedule numExistingPods pods before the benchmark starts, and at
// least minPods pods during the benchmark.
func benchmarkScheduling(numNodes, numExistingPods, minPods int,
	nodeStrategy testutils.PrepareNodeStrategy,
	setupPodStrategy, testPodStrategy testutils.TestPodCreateStrategy,
	b *testing.B) {
	if b.N < minPods {
		b.N = minPods
	}
	schedulerConfigFactory, finalFunc := mustSetupScheduler()
	defer finalFunc()
	c := schedulerConfigFactory.GetClient()

	nodePreparer := framework.NewIntegrationTestNodePreparer(
		c,
		[]testutils.CountToStrategy{{Count: numNodes, Strategy: nodeStrategy}},
		"scheduler-perf-",
	)
	if err := nodePreparer.PrepareNodes(); err != nil {
		klog.Fatalf("%v", err)
	}
	defer nodePreparer.CleanupNodes()

	config := testutils.NewTestPodCreatorConfig()
	config.AddStrategy("sched-test", numExistingPods, setupPodStrategy)
	podCreator := testutils.NewTestPodCreator(c, config)
	podCreator.CreatePods()

	for {
		scheduled, err := schedulerConfigFactory.GetScheduledPodLister().List(labels.Everything())
		if err != nil {
			klog.Fatalf("%v", err)
		}
		if len(scheduled) >= numExistingPods {
			break
		}
		time.Sleep(1 * time.Second)
	}
	// start benchmark
	b.ResetTimer()
	config = testutils.NewTestPodCreatorConfig()
	config.AddStrategy("sched-test", b.N, testPodStrategy)
	podCreator = testutils.NewTestPodCreator(c, config)
	podCreator.CreatePods()
	for {
		// This can potentially affect performance of scheduler, since List() is done under mutex.
		// TODO: Setup watch on apiserver and wait until all pods scheduled.
		scheduled, err := schedulerConfigFactory.GetScheduledPodLister().List(labels.Everything())
		if err != nil {
			klog.Fatalf("%v", err)
		}
		if len(scheduled) >= numExistingPods+b.N {
			break
		}
		// Note: This might introduce slight deviation in accuracy of benchmark results.
		// Since the total amount of time is relatively large, it might not be a concern.
		time.Sleep(100 * time.Millisecond)
	}
}
