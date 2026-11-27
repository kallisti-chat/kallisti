package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strconv"

	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

var ErrNotReady = errors.New("candidate is no longer ready")

const managedKey = "app.kubernetes.io/managed-by"
const managedValue = "kallisti-preview-controller"
const slotKey = "preview.kallisti.fans/slot"
const revisionKey = "preview.kallisti.fans/revision"
const selector = managedKey + "=" + managedValue

var routes = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}

func ptr[T any](v T) *T { return &v }
func labels(i int, revision string) map[string]string {
	m := map[string]string{managedKey: managedValue, slotKey: strconv.Itoa(i + 1)}
	if revision != "" {
		m[revisionKey] = revision
	}
	return m
}

type Kubernetes struct {
	Client       kubernetes.Interface
	Dynamic      dynamic.Interface
	Bootstrap    string
	PodCIDRs     []netip.Prefix
	stateVersion string
}

func (k *Kubernetes) Load(ctx context.Context) (*State, error) {
	cm, err := k.Client.CoreV1().ConfigMaps("preview-system").Get(ctx, "preview-state", meta.GetOptions{})
	if apierrors.IsNotFound(err) {
		k.stateVersion = ""
		return &State{}, nil
	}
	if err != nil {
		return nil, err
	}
	var s State
	if err = json.Unmarshal([]byte(cm.Data["state.json"]), &s); err != nil {
		return nil, err
	}
	if err := s.validate(); err != nil {
		return nil, err
	}
	k.stateVersion = cm.ResourceVersion
	return &s, nil
}
func (k *Kubernetes) Save(ctx context.Context, s *State) error {
	if err := s.validate(); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	cm := &core.ConfigMap{ObjectMeta: meta.ObjectMeta{Name: "preview-state", Namespace: "preview-system", ResourceVersion: k.stateVersion}, Data: map[string]string{"state.json": string(b)}}
	var result *core.ConfigMap
	if k.stateVersion == "" {
		result, err = k.Client.CoreV1().ConfigMaps("preview-system").Create(ctx, cm, meta.CreateOptions{})
	} else {
		result, err = k.Client.CoreV1().ConfigMaps("preview-system").Update(ctx, cm, meta.UpdateOptions{})
	}
	if err == nil {
		k.stateVersion = result.ResourceVersion
	}
	return err
}
func resources() core.ResourceRequirements {
	return core.ResourceRequirements{Requests: core.ResourceList{core.ResourceCPU: resource.MustParse("100m"), core.ResourceMemory: resource.MustParse("128Mi"), core.ResourceEphemeralStorage: resource.MustParse("1Gi")}, Limits: core.ResourceList{core.ResourceCPU: resource.MustParse("500m"), core.ResourceMemory: resource.MustParse("256Mi"), core.ResourceEphemeralStorage: resource.MustParse("2Gi")}}
}
func security() *core.SecurityContext {
	return &core.SecurityContext{AllowPrivilegeEscalation: ptr(false), ReadOnlyRootFilesystem: ptr(true), Capabilities: &core.Capabilities{Drop: []core.Capability{"ALL"}}}
}
func (k *Kubernetes) deployment(i int, r Revision) *apps.Deployment {
	name := r.Evidence.Revision()
	ls := labels(i, name)
	env := []core.EnvVar{
		{Name: "DB_PATH", Value: "/data/kallisti.db"},
		{Name: "UPLOAD_DIR", Value: "/data/uploads"},
		{Name: "SERVER_NAME", Value: fmt.Sprintf("PREVIEW PR #%d — disposable", r.Evidence.PR)},
		{Name: "ALLOWED_ORIGINS", Value: "https://" + hostname(r.Evidence.PR)},
		{Name: "TRUST_FORWARDED_FOR", Value: "1"},
		{Name: "MUMBLE_PUBLIC_PORT", Value: "0"},
		{Name: "MUMBLE_CERT_FILE", Value: "/data/cert.pem"},
		{Name: "MUMBLE_KEY_FILE", Value: "/data/key.pem"},
		{Name: "LISTEN_PORT", Value: "8080"},
		{Name: "PUSH", Value: "0"},
		{Name: "TMPDIR", Value: "/data/tmp"},
	}
	pod := core.PodSpec{
		ServiceAccountName:           "preview-workload",
		AutomountServiceAccountToken: ptr(false),
		SecurityContext: &core.PodSecurityContext{
			RunAsNonRoot: ptr(true), RunAsUser: ptr(int64(1000)), RunAsGroup: ptr(int64(1000)), FSGroup: ptr(int64(1000)),
			SeccompProfile: &core.SeccompProfile{Type: core.SeccompProfileTypeRuntimeDefault},
		},
		Volumes: []core.Volume{
			{Name: "data", VolumeSource: core.VolumeSource{EmptyDir: &core.EmptyDirVolumeSource{SizeLimit: ptr(resource.MustParse("1Gi"))}}},
			{Name: "accounts", VolumeSource: core.VolumeSource{Secret: &core.SecretVolumeSource{
				SecretName: "preview-accounts", DefaultMode: ptr(int32(0440)),
				Items: []core.KeyToPath{{Key: "lambda", Path: "lambda"}, {Key: "cortex", Path: "cortex"}},
			}}},
		},
		InitContainers: []core.Container{{
			Name: "accounts", Image: k.Bootstrap, Command: []string{"/preview-init"}, Args: []string{"/data", "/accounts"},
			Resources: resources(), SecurityContext: security(),
			VolumeMounts: []core.VolumeMount{{Name: "data", MountPath: "/data"}, {Name: "accounts", MountPath: "/accounts", ReadOnly: true}},
		}},
		Containers: []core.Container{{
			Name: "app", Image: r.Evidence.Image + "@" + r.Evidence.Digest, Env: env,
			Resources: resources(), SecurityContext: security(),
			Ports:        []core.ContainerPort{{Name: "http", ContainerPort: 8080}},
			VolumeMounts: []core.VolumeMount{{Name: "data", MountPath: "/data"}},
			ReadinessProbe: &core.Probe{
				ProbeHandler:        core.ProbeHandler{HTTPGet: &core.HTTPGetAction{Path: "/", Port: intstr.FromInt32(8080)}},
				InitialDelaySeconds: 3, PeriodSeconds: 5, FailureThreshold: 3,
			},
		}},
	}
	return &apps.Deployment{
		ObjectMeta: meta.ObjectMeta{Name: name, Namespace: slotName(i), Labels: ls},
		Spec: apps.DeploymentSpec{
			Replicas: ptr(int32(1)), Strategy: apps.DeploymentStrategy{Type: apps.RecreateDeploymentStrategyType},
			Selector: &meta.LabelSelector{MatchLabels: ls},
			Template: core.PodTemplateSpec{ObjectMeta: meta.ObjectMeta{Labels: ls}, Spec: pod},
		},
	}
}
func (k *Kubernetes) Ensure(ctx context.Context, i int, r Revision) (bool, error) {
	desired := k.deployment(i, r)
	client := k.Client.AppsV1().Deployments(slotName(i))
	d, err := client.Get(ctx, desired.Name, meta.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = client.Create(ctx, desired, meta.CreateOptions{})
		return false, err
	}
	if err != nil {
		return false, err
	}
	if d.Labels[managedKey] != managedValue || d.Labels[revisionKey] != desired.Name || len(d.Spec.Template.Spec.Containers) != 1 || d.Spec.Template.Spec.Containers[0].Image != desired.Spec.Template.Spec.Containers[0].Image {
		return false, fmt.Errorf("deployment identity mismatch")
	}
	p, err := k.readyPod(ctx, i, r)
	return p != nil, err
}
func (k *Kubernetes) readyPod(ctx context.Context, i int, r Revision) (*core.Pod, error) {
	ns := slotName(i)
	d, err := k.Client.AppsV1().Deployments(ns).Get(ctx, r.Evidence.Revision(), meta.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if d.DeletionTimestamp != nil || d.Labels[managedKey] != managedValue || d.Labels[slotKey] != strconv.Itoa(i+1) {
		return nil, nil
	}
	pods, err := k.Client.CoreV1().Pods(ns).List(ctx, meta.ListOptions{LabelSelector: selector + "," + revisionKey + "=" + d.Name})
	if err != nil {
		return nil, err
	}
	for _, p := range pods.Items {
		if p.DeletionTimestamp != nil || p.Status.Phase != core.PodRunning || p.Labels[slotKey] != strconv.Itoa(i+1) || p.Spec.ServiceAccountName != "preview-workload" || p.Spec.AutomountServiceAccountToken == nil || *p.Spec.AutomountServiceAccountToken {
			continue
		}
		if len(p.Spec.Containers) != 1 || p.Spec.Containers[0].Image != r.Evidence.Image+"@"+r.Evidence.Digest {
			continue
		}
		ready := false
		for _, c := range p.Status.Conditions {
			if c.Type == core.PodReady && c.Status == core.ConditionTrue {
				ready = true
			}
		}
		if !ready {
			continue
		}
		owner := meta.GetControllerOf(&p)
		if owner == nil || owner.Kind != "ReplicaSet" || owner.APIVersion != "apps/v1" {
			continue
		}
		rs, err := k.Client.AppsV1().ReplicaSets(ns).Get(ctx, owner.Name, meta.GetOptions{})
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if rs.UID != owner.UID || rs.DeletionTimestamp != nil {
			continue
		}
		parent := meta.GetControllerOf(rs)
		if parent == nil || parent.Kind != "Deployment" || parent.APIVersion != "apps/v1" || parent.UID != d.UID || parent.Name != d.Name {
			continue
		}
		ip, err := netip.ParseAddr(p.Status.PodIP)
		if err != nil || !ip.Is4() {
			continue
		}
		allowed := false
		for _, cidr := range k.PodCIDRs {
			if cidr.Contains(ip) {
				allowed = true
			}
		}
		if !allowed {
			continue
		}
		return &p, nil
	}
	return nil, nil
}
func (k *Kubernetes) Publish(ctx context.Context, i, pr int, r *Revision, requireReady bool) error {
	p, err := k.readyPod(ctx, i, *r)
	if err != nil {
		return err
	}
	if requireReady && p == nil {
		return ErrNotReady
	}
	ns, name := "preview-edge", slotName(i)
	svcClient := k.Client.CoreV1().Services(ns)
	svc, err := svcClient.Get(ctx, name, meta.GetOptions{})
	if apierrors.IsNotFound(err) {
		svc, err = svcClient.Create(ctx, &core.Service{ObjectMeta: meta.ObjectMeta{Name: name, Labels: labels(i, "")}, Spec: core.ServiceSpec{Ports: []core.ServicePort{{Name: "http", Port: 80, TargetPort: intstr.FromInt32(8080)}}}}, meta.CreateOptions{})
	}
	if err != nil {
		return err
	}
	if svc != nil && svc.Labels[managedKey] != managedValue {
		return fmt.Errorf("foreign edge service")
	}
	slice := &discovery.EndpointSlice{ObjectMeta: meta.ObjectMeta{Name: name, Labels: labels(i, "")}, AddressType: discovery.AddressTypeIPv4, Ports: []discovery.EndpointPort{{Name: ptr("http"), Protocol: ptr(core.ProtocolTCP), Port: ptr(int32(8080))}}, Endpoints: []discovery.Endpoint{}}
	slice.Labels[discovery.LabelServiceName] = name
	slice.Labels[discovery.LabelManagedBy] = managedValue
	if p != nil {
		slice.Endpoints = []discovery.Endpoint{{Addresses: []string{p.Status.PodIP}, Conditions: discovery.EndpointConditions{Ready: ptr(true)}, TargetRef: &core.ObjectReference{APIVersion: "v1", Kind: "Pod", Namespace: p.Namespace, Name: p.Name, UID: p.UID}}}
	}
	slicesClient := k.Client.DiscoveryV1().EndpointSlices(ns)
	old, err := slicesClient.Get(ctx, name, meta.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = slicesClient.Create(ctx, slice, meta.CreateOptions{})
	} else if err == nil {
		if old.Labels[managedKey] != managedValue {
			return fmt.Errorf("foreign endpoint slice")
		}
		slice.ResourceVersion = old.ResourceVersion
		_, err = slicesClient.Update(ctx, slice, meta.UpdateOptions{})
	}
	if err != nil {
		return err
	}
	routeName := fmt.Sprintf("preview-pr-%d", pr)
	route := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "gateway.networking.k8s.io/v1", "kind": "HTTPRoute", "metadata": map[string]any{"name": routeName, "namespace": ns, "labels": map[string]any{managedKey: managedValue, slotKey: strconv.Itoa(i + 1)}}, "spec": map[string]any{"parentRefs": []any{map[string]any{"name": "public", "sectionName": "https"}}, "hostnames": []any{hostname(pr)}, "rules": []any{map[string]any{"backendRefs": []any{map[string]any{"name": name, "port": int64(80)}}}}}}}
	rc := k.Dynamic.Resource(routes).Namespace(ns)
	oldRoute, err := rc.Get(ctx, routeName, meta.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = rc.Create(ctx, route, meta.CreateOptions{})
	} else if err == nil {
		if oldRoute.GetLabels()[managedKey] != managedValue {
			return fmt.Errorf("foreign route")
		}
		route.SetResourceVersion(oldRoute.GetResourceVersion())
		_, err = rc.Update(ctx, route, meta.UpdateOptions{})
	}
	return err
}
func (k *Kubernetes) Withdraw(ctx context.Context, i, pr int) error {
	// require ownership and uid preconditions so a replaced object is never deleted.
	rc := k.Dynamic.Resource(routes).Namespace("preview-edge")
	route, err := rc.Get(ctx, fmt.Sprintf("preview-pr-%d", pr), meta.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if err == nil {
		if route.GetLabels()[managedKey] != managedValue || route.GetLabels()[slotKey] != strconv.Itoa(i+1) {
			return fmt.Errorf("foreign route")
		}
		uid := route.GetUID()
		if err = rc.Delete(ctx, route.GetName(), meta.DeleteOptions{Preconditions: &meta.Preconditions{UID: &uid}}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	sc := k.Client.DiscoveryV1().EndpointSlices("preview-edge")
	slice, err := sc.Get(ctx, slotName(i), meta.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if err == nil {
		if slice.Labels[managedKey] != managedValue || slice.Labels[slotKey] != strconv.Itoa(i+1) {
			return fmt.Errorf("foreign endpoint slice")
		}
		if err = sc.Delete(ctx, slice.Name, meta.DeleteOptions{Preconditions: &meta.Preconditions{UID: &slice.UID}}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}
func (k *Kubernetes) Prune(ctx context.Context, i int, keep []string) (bool, error) {
	ds, err := k.Client.AppsV1().Deployments(slotName(i)).List(ctx, meta.ListOptions{LabelSelector: selector})
	if err != nil {
		return false, err
	}
	done := true
	for _, d := range ds.Items {
		if slices.Contains(keep, d.Name) {
			continue
		}
		done = false
		if d.DeletionTimestamp != nil {
			continue
		}
		uid := d.UID
		err = k.Client.AppsV1().Deployments(slotName(i)).Delete(ctx, d.Name, meta.DeleteOptions{PropagationPolicy: ptr(meta.DeletePropagationForeground), Preconditions: &meta.Preconditions{UID: &uid}})
		if err != nil && !apierrors.IsNotFound(err) {
			return false, err
		}
	}
	ps, err := k.Client.CoreV1().Pods(slotName(i)).List(ctx, meta.ListOptions{LabelSelector: selector})
	if err != nil {
		return false, err
	}
	for _, p := range ps.Items {
		if !slices.Contains(keep, p.Labels[revisionKey]) {
			done = false
		}
	}
	return done, nil
}
