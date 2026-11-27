package controller

import (
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func kubeFixture(t *testing.T) (*Kubernetes, Revision, *core.Pod) {
	t.Helper()
	ev := evidenceFor(31, "a")
	r := Revision{Evidence: ev}
	k := &Kubernetes{Bootstrap: "ghcr.io/kallisti-chat/kallisti-preview-controller@sha256:" + strings.Repeat("b", 64), PodCIDRs: []netip.Prefix{netip.MustParsePrefix("10.248.0.0/16")}}
	d := k.deployment(0, r)
	d.UID = "deployment-uid"
	rs := &apps.ReplicaSet{ObjectMeta: meta.ObjectMeta{Name: d.Name + "-rs", Namespace: slotName(0), UID: "rs-uid", OwnerReferences: []meta.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: d.Name, UID: d.UID, Controller: ptr(true)}}}}
	p := &core.Pod{ObjectMeta: meta.ObjectMeta{Name: rs.Name + "-pod", Namespace: slotName(0), UID: "pod-uid", Labels: labels(0, d.Name), OwnerReferences: []meta.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: ptr(true)}}}, Spec: *d.Spec.Template.Spec.DeepCopy(), Status: core.PodStatus{Phase: core.PodRunning, PodIP: "10.248.1.5", Conditions: []core.PodCondition{{Type: core.PodReady, Status: core.ConditionTrue}}}}
	k.Client = fake.NewClientset(d, rs, p)
	k.Dynamic = dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	return k, r, p
}
func TestEndpointOwnershipAndPodRecreation(t *testing.T) {
	ctx := context.Background()
	k, r, p := kubeFixture(t)
	if err := k.Publish(ctx, 0, 31, &r, false); err != nil {
		t.Fatal(err)
	}
	check := func(want string) {
		t.Helper()
		s, err := k.Client.DiscoveryV1().EndpointSlices("preview-edge").Get(ctx, "preview-slot-1", meta.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if want == "" {
			if len(s.Endpoints) != 0 {
				t.Fatal("stale endpoint retained")
			}
			return
		}
		if len(s.Endpoints) != 1 || s.Endpoints[0].Addresses[0] != want || s.Endpoints[0].TargetRef.UID != p.UID {
			t.Fatalf("bad endpoint: %+v", s.Endpoints)
		}
	}
	check(p.Status.PodIP)
	p.Status.PodIP = "10.248.1.6"
	p.UID = "new-pod-uid"
	if _, err := k.Client.CoreV1().Pods(p.Namespace).Update(ctx, p, meta.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := k.Publish(ctx, 0, 31, &r, false); err != nil {
		t.Fatal(err)
	}
	check(p.Status.PodIP)
	p.OwnerReferences[0].UID = types.UID("forged-rs")
	if _, err := k.Client.CoreV1().Pods(p.Namespace).Update(ctx, p, meta.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := k.Publish(ctx, 0, 31, &r, false); err != nil {
		t.Fatal(err)
	}
	check("")
}
func TestUntrustedPodNeverRouted(t *testing.T) {
	for name, mutate := range map[string]func(*core.Pod){"wrong-slot": func(p *core.Pod) { p.Labels[slotKey] = "2" }, "terminating": func(p *core.Pod) { p.DeletionTimestamp = ptr(meta.Now()) }, "public-ip": func(p *core.Pod) { p.Status.PodIP = "8.8.8.8" }, "token": func(p *core.Pod) { p.Spec.AutomountServiceAccountToken = ptr(true) }, "wrong-image": func(p *core.Pod) { p.Spec.Containers[0].Image = "evil" }, "not-ready": func(p *core.Pod) { p.Status.Conditions = nil }} {
		t.Run(name, func(t *testing.T) {
			k, r, p := kubeFixture(t)
			mutate(p)
			if _, err := k.Client.CoreV1().Pods(p.Namespace).Update(context.Background(), p, meta.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			got, err := k.readyPod(context.Background(), 0, r)
			if err != nil {
				t.Fatal(err)
			}
			if got != nil {
				t.Fatal("untrusted pod selected")
			}
			if err = k.Publish(context.Background(), 0, 31, &r, true); err != ErrNotReady {
				t.Fatalf("candidate published: %v", err)
			}
		})
	}
}
func TestDeploymentIsolationAndRenderFixtures(t *testing.T) {
	k, r, _ := kubeFixture(t)
	d := k.deployment(0, r)
	p := d.Spec.Template.Spec
	if len(p.Volumes) != 2 || len(p.Containers[0].VolumeMounts) != 1 || p.Containers[0].VolumeMounts[0].Name != "data" || *p.AutomountServiceAccountToken || p.ServiceAccountName != "preview-workload" {
		t.Fatal("runtime isolation changed")
	}
	if p.InitContainers[0].Image != k.Bootstrap || p.InitContainers[0].VolumeMounts[1].Name != "accounts" {
		t.Fatal("untrusted bootstrap")
	}
	if got := p.Containers[0].Resources.Limits.Memory().String(); got != "256Mi" {
		t.Fatal(got)
	}
	// optional export feeds the infrastructure repository's real api admission test.
	if dir := os.Getenv("PREVIEW_FIXTURE_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		d.APIVersion = "apps/v1"
		d.Kind = "Deployment"
		write := func(name string, v any) {
			t.Helper()
			b, err := json.MarshalIndent(v, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, name+".json"), b, 0600); err != nil {
				t.Fatal(err)
			}
		}
		write("deployment", d)
		if err := k.Publish(context.Background(), 0, 31, &r, false); err != nil {
			t.Fatal(err)
		}
		svc, err := k.Client.CoreV1().Services("preview-edge").Get(context.Background(), "preview-slot-1", meta.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		svc.APIVersion = "v1"
		svc.Kind = "Service"
		svc.Namespace = "preview-edge"
		write("service", svc)
		s, err := k.Client.DiscoveryV1().EndpointSlices("preview-edge").Get(context.Background(), "preview-slot-1", meta.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		s.APIVersion = "discovery.k8s.io/v1"
		s.Kind = "EndpointSlice"
		s.Namespace = "preview-edge"
		write("slice", s)
		route, err := k.Dynamic.Resource(routes).Namespace("preview-edge").Get(context.Background(), "preview-pr-31", meta.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		write("route", route)
	}
}
func TestStateRoundTrip(t *testing.T) {
	ctx := context.Background()
	k, _, _ := kubeFixture(t)
	if err := k.Save(ctx, &State{}); err != nil {
		t.Fatal(err)
	}
	loaded, err := k.Load(ctx)
	if err != nil || loaded == nil {
		t.Fatalf("state load: %v", err)
	}
	k.stateVersion = "stale"
	client := k.Client.(*fake.Clientset)
	client.PrependReactor("update", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		cm := action.(ktesting.UpdateAction).GetObject().(*core.ConfigMap)
		if cm.ResourceVersion != "stale" {
			t.Fatal("optimistic lock missing")
		}
		return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, cm.Name, nil)
	})
	if err = k.Save(ctx, &State{Records: map[int]*Record{1: {Status: "changed"}}}); !apierrors.IsConflict(err) {
		t.Fatalf("conflict hidden: %v", err)
	}
	if k.stateVersion != "stale" {
		t.Fatal("conflict changed resource version")
	}
	cm, err := client.CoreV1().ConfigMaps("preview-system").Get(ctx, "preview-state", meta.GetOptions{})
	if err != nil || strings.Contains(cm.Data["state.json"], "changed") {
		t.Fatal("conflicting write overwrote state")
	}
	if err = client.CoreV1().ConfigMaps("preview-system").Delete(ctx, "preview-state", meta.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err = k.Load(ctx); err != nil || k.stateVersion != "" {
		t.Fatal("missing state retained stale version")
	}
	if err = k.Save(ctx, &State{}); err != nil {
		t.Fatal(err)
	}
}

func TestWithdrawPreservesForeignRoute(t *testing.T) {
	ctx := context.Background()
	k, r, _ := kubeFixture(t)
	if err := k.Publish(ctx, 0, 31, &r, false); err != nil {
		t.Fatal(err)
	}
	rc := k.Dynamic.Resource(routes).Namespace("preview-edge")
	route, err := rc.Get(ctx, "preview-pr-31", meta.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	route.SetLabels(map[string]string{managedKey: "someone-else"})
	if _, err = rc.Update(ctx, route, meta.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err = k.Withdraw(ctx, 0, 31); err == nil {
		t.Fatal("foreign route deleted")
	}
	if _, err = rc.Get(ctx, route.GetName(), meta.GetOptions{}); err != nil {
		t.Fatal(err)
	}
	route.SetLabels(labels(0, ""))
	if _, err = rc.Update(ctx, route, meta.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err = k.Withdraw(ctx, 0, 31); err != nil {
		t.Fatal(err)
	}
	if err = k.Withdraw(ctx, 0, 31); err != nil {
		t.Fatal("withdraw not idempotent", err)
	}
}
func TestLoadRejectsCorruptState(t *testing.T) {
	k, _, _ := kubeFixture(t)
	ctx := context.Background()
	cm := &core.ConfigMap{ObjectMeta: meta.ObjectMeta{Name: "preview-state", Namespace: "preview-system"}, Data: map[string]string{"state.json": `{"slots":[{"pr":1,"active":{"evidence":{"sha":"x"}}}]}`}}
	if _, err := k.Client.CoreV1().ConfigMaps("preview-system").Create(ctx, cm, meta.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Load(ctx); err == nil {
		t.Fatal("corrupt state loaded")
	}
}

func TestWithdrawPreservesForeignSlice(t *testing.T) {
	ctx := context.Background()
	k, r, _ := kubeFixture(t)
	if err := k.Publish(ctx, 0, 31, &r, false); err != nil {
		t.Fatal(err)
	}
	sc := k.Client.DiscoveryV1().EndpointSlices("preview-edge")
	slice, err := sc.Get(ctx, "preview-slot-1", meta.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	slice.Labels[slotKey] = "2"
	if _, err = sc.Update(ctx, slice, meta.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err = k.Withdraw(ctx, 0, 31); err == nil {
		t.Fatal("foreign slice deleted")
	}
	if _, err = sc.Get(ctx, slice.Name, meta.GetOptions{}); err != nil {
		t.Fatal(err)
	}
}
