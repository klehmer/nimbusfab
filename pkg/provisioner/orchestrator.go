package provisioner

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/klehmer/nimbusfab/pkg/ir"
)

// topoSort returns components in dependency order: if B references A, A appears
// before B. Cycles return an error.
func topoSort(components []ir.Component) ([]ir.Component, error) {
	byName := make(map[string]ir.Component, len(components))
	for _, c := range components {
		if _, dup := byName[c.Name]; dup {
			return nil, fmt.Errorf("topoSort: duplicate component %q", c.Name)
		}
		byName[c.Name] = c
	}
	indeg := make(map[string]int, len(components))
	children := make(map[string][]string, len(components))
	for _, c := range components {
		indeg[c.Name] += 0
		for _, ref := range c.Refs {
			if _, ok := byName[ref.Component]; !ok {
				return nil, fmt.Errorf("topoSort: component %q references unknown %q", c.Name, ref.Component)
			}
			indeg[c.Name]++
			children[ref.Component] = append(children[ref.Component], c.Name)
		}
	}
	queue := []string{}
	for name, d := range indeg {
		if d == 0 {
			queue = append(queue, name)
		}
	}
	sort.Strings(queue)
	var out []ir.Component
	for len(queue) > 0 {
		head := queue[0]
		queue = queue[1:]
		out = append(out, byName[head])
		kids := append([]string{}, children[head]...)
		sort.Strings(kids)
		for _, k := range kids {
			indeg[k]--
			if indeg[k] == 0 {
				queue = append(queue, k)
			}
		}
	}
	if len(out) != len(components) {
		return nil, fmt.Errorf("topoSort: cycle detected (%d of %d components resolved)", len(out), len(components))
	}
	return out, nil
}

// targetWorker is the per-target function the orchestrator invokes.
type targetWorker func(ctx context.Context, component ir.Component, target ir.DeploymentTarget) TargetApplyResult

// concurrencyCaps captures global / per-cloud / per-credential limits.
type concurrencyCaps struct {
	Global        int
	PerCloud      int
	PerCredential int
}

func resolveCaps(in concurrencyCaps) concurrencyCaps {
	out := in
	if out.Global <= 0 {
		out.Global = runtime.NumCPU()
	}
	if out.PerCloud <= 0 {
		out.PerCloud = 8
	}
	if out.PerCredential <= 0 {
		out.PerCredential = 4
	}
	return out
}

// semaphores holds the three named semaphores. Acquisition order is always
// global -> cloud -> credential to avoid deadlock.
type semaphores struct {
	global                  chan struct{}
	perCloud                map[string]chan struct{}
	perCred                 map[string]chan struct{}
	capPerCloud, capPerCred int
	mu                      sync.Mutex
}

func newSemaphores(caps concurrencyCaps) *semaphores {
	return &semaphores{
		global:      make(chan struct{}, caps.Global),
		perCloud:    map[string]chan struct{}{},
		perCred:     map[string]chan struct{}{},
		capPerCloud: caps.PerCloud,
		capPerCred:  caps.PerCredential,
	}
}

func (s *semaphores) acquire(ctx context.Context, cloud, cred string) (release func(), err error) {
	select {
	case s.global <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	s.mu.Lock()
	cs, ok := s.perCloud[cloud]
	if !ok {
		cs = make(chan struct{}, s.capPerCloud)
		s.perCloud[cloud] = cs
	}
	crs, ok := s.perCred[cred]
	if !ok {
		crs = make(chan struct{}, s.capPerCred)
		s.perCred[cred] = crs
	}
	s.mu.Unlock()
	select {
	case cs <- struct{}{}:
	case <-ctx.Done():
		<-s.global
		return nil, ctx.Err()
	}
	select {
	case crs <- struct{}{}:
	case <-ctx.Done():
		<-cs
		<-s.global
		return nil, ctx.Err()
	}
	return func() {
		<-crs
		<-cs
		<-s.global
	}, nil
}

// runComponent runs one component's targets according to its policy and the
// concurrency caps.
func runComponent(ctx context.Context, comp ir.Component, sems *semaphores, work targetWorker) []TargetApplyResult {
	results := make([]TargetApplyResult, len(comp.Targets))
	if comp.Policy.Serial {
		for i, t := range comp.Targets {
			results[i] = runOne(ctx, comp, t, sems, work)
		}
		return results
	}
	var wg sync.WaitGroup
	for i, t := range comp.Targets {
		wg.Add(1)
		go func(i int, t ir.DeploymentTarget) {
			defer wg.Done()
			results[i] = runOne(ctx, comp, t, sems, work)
		}(i, t)
	}
	wg.Wait()
	return results
}

func runOne(ctx context.Context, comp ir.Component, t ir.DeploymentTarget, sems *semaphores, work targetWorker) TargetApplyResult {
	rel, err := sems.acquire(ctx, t.Cloud, t.CredentialRef)
	if err != nil {
		return TargetApplyResult{
			Component:  comp.Name,
			Cloud:      t.Cloud,
			Region:     t.Region,
			Status:     RunStatusFailed,
			Error:      err,
			StartedAt:  time.Now().UTC(),
			FinishedAt: time.Now().UTC(),
		}
	}
	defer rel()
	return work(ctx, comp, t)
}

// hasFailedUpstream returns true if any of comp's Refs are in the failed set.
func hasFailedUpstream(comp ir.Component, failed map[string]bool) bool {
	for _, ref := range comp.Refs {
		if failed[ref.Component] {
			return true
		}
	}
	return false
}

func anyFailed(rs []TargetApplyResult) bool {
	for _, r := range rs {
		if r.Status == RunStatusFailed {
			return true
		}
	}
	return false
}
