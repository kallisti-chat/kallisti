package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"kallisti.chat/preview/internal/controller"
)

func main() {
	if err := run(); err != nil {
		slog.Error("controller stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	id, err := strconv.ParseInt(os.Getenv("GITHUB_WORKFLOW_ID"), 10, 64)
	if err != nil || id <= 0 {
		return fmt.Errorf("GITHUB_WORKFLOW_ID must identify installed preview.yml")
	}
	bot := os.Getenv("GITHUB_BOT_LOGIN")
	if bot == "" {
		return fmt.Errorf("GITHUB_BOT_LOGIN required")
	}
	tokenFile := os.Getenv("GITHUB_TOKEN_FILE")
	if tokenFile == "" {
		return fmt.Errorf("GITHUB_TOKEN_FILE required")
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return err
	}
	config.Timeout = 15 * time.Second
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}
	dyn, err := dynamic.NewForConfig(config)
	if err != nil {
		return err
	}
	cidrs := []netip.Prefix{}
	for _, v := range strings.Split(os.Getenv("POD_CIDRS"), ",") {
		p, err := netip.ParsePrefix(v)
		if err != nil {
			return fmt.Errorf("POD_CIDRS: %w", err)
		}
		cidrs = append(cidrs, p)
	}
	identity, err := os.Hostname()
	if err != nil {
		return err
	}
	identity += "-" + strconv.Itoa(os.Getpid())
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	lock := &resourcelock.LeaseLock{LeaseMeta: meta.ObjectMeta{Name: "preview-controller", Namespace: "preview-system"}, Client: client.CoordinationV1(), LockConfig: resourcelock.ResourceLockConfig{Identity: identity}}

	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{Lock: lock, LeaseDuration: 60 * time.Second, RenewDeadline: 40 * time.Second, RetryPeriod: 10 * time.Second, ReleaseOnCancel: false, Callbacks: leaderelection.LeaderCallbacks{OnStartedLeading: func(leader context.Context) {
		cm, err := client.CoreV1().ConfigMaps("preview-system").Get(leader, "preview-admission", meta.GetOptions{})
		if err != nil {
			slog.Error("bootstrap admission config", "error", err)
			stop()
			return
		}
		image := cm.Data["bootstrapImage"]
		if !regexp.MustCompile(`^[a-z0-9][a-z0-9./:_-]*@sha256:[0-9a-f]{64}$`).MatchString(image) {
			slog.Error("bootstrapImage must be digest pinned")
			stop()
			return
		}
		runtime := &controller.Kubernetes{Client: client, Dynamic: dyn, Bootstrap: image, PodCIDRs: cidrs}
		state, err := runtime.Load(leader)
		if err != nil {
			slog.Error("load state", "error", err)
			stop()
			return
		}
		engine := controller.Engine{State: state, Runtime: runtime, Save: runtime.Save, Now: time.Now}
		ticker := time.NewTicker(45 * time.Second)
		defer ticker.Stop()
		for {
			if err := cycle(leader, &engine, tokenFile, id, bot); err != nil {
				slog.Error("reconcile failed; retrying", "error", err)
			}
			select {
			case <-leader.Done():
				return
			case <-ticker.C:
			}
		}
	}, OnStoppedLeading: func() { slog.Info("leadership ended"); stop() }}})
	return nil
}
func cycle(ctx context.Context, e *controller.Engine, tokenFile string, id int64, bot string) error {
	runtime := e.Runtime.(*controller.Kubernetes)
	state, err := runtime.Load(ctx)
	if err != nil {
		return err
	}
	e.State = state
	b, err := os.ReadFile(tokenFile)
	if err != nil {
		return err
	}
	token := strings.TrimSpace(string(b))
	if token == "" {
		return fmt.Errorf("GitHub credential is empty")
	}
	g := controller.NewGitHub(token, id, bot)
	prs, err := g.Pulls(ctx)
	if err != nil {
		return err
	}
	evidence, err := g.Evidence(ctx, prs)
	if err != nil {
		return err
	}
	// refresh after metadata queries so slow API calls cannot deploy an old snapshot.
	prs, err = g.Pulls(ctx)
	if err != nil {
		return err
	}
	if err = e.Tick(ctx, prs, evidence); err != nil {
		return err
	}
	for n, r := range e.State.Records {
		body := fmt.Sprintf("Preview: %s\n\nURL: https://pr-%d.kallisti.fans\n\nDisposable private test data; expires after 72 hours. Browser voice only; screen sharing is not qualified.", r.Status, n)
		for _, s := range e.State.Slots {
			if s.PR == n && s.Active != nil {
				body += "\n\nServing SHA: `" + s.Active.Evidence.SHA + "`"
			}
		}
		if err = g.Comment(ctx, n, r, body); err != nil {
			return err
		}
		if err = e.Save(ctx, e.State); err != nil {
			return err
		}
	}
	return nil
}
