package sp

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/showwin/speedtest-go/speedtest"
)

const customSpeedtestPreloadDeadline = 20 * time.Second

// CustomSpeedTestPreload prepares a legacy custom speedtest source without
// starting a download or upload measurement. Call one of its Run methods only
// after Wait has completed so candidate probe traffic cannot affect throughput.
type CustomSpeedTestPreload struct {
	done         chan struct{}
	targets      speedtest.Servers
	client       *speedtest.Speedtest
	usedFallback bool
	network      string
	err          error
}

// StartCustomSpeedTestPreload starts source loading, target parsing, and
// latency ranking in the background. It keeps the caller's source URL and
// address-family policy intact, and bounds candidate work independently from a
// later throughput stage.
func StartCustomSpeedTestPreload(ctx context.Context, url, byWhat, network string) *CustomSpeedTestPreload {
	if ctx == nil {
		ctx = context.Background()
	}
	parentCtx := ctx
	preloadCtx, cancel := context.WithTimeout(parentCtx, customSpeedtestPreloadDeadline)
	preload := &CustomSpeedTestPreload{
		done:    make(chan struct{}),
		network: network,
	}
	go func() {
		defer close(preload.done)
		defer cancel()
		if err := parentCtx.Err(); err != nil {
			preload.err = err
			return
		}
		preload.client = speedtestClientForNetwork(network)
		data := getDataWithNetworkContext(preloadCtx, url, network)
		if err := parentCtx.Err(); err != nil {
			preload.err = err
			return
		}
		preload.targets, preload.usedFallback = customSpeedtestTargetsFromData(preloadCtx, data, url, byWhat, preload.client, network)
		if len(preload.targets) == 0 {
			preload.err = fmt.Errorf("no custom speedtest candidates available")
			return
		}
		preload.targets = rankSpeedtestTargetsByLatencyConcurrent(preloadCtx, preload.targets, preload.usedFallback)
		if err := parentCtx.Err(); err != nil {
			preload.err = err
			return
		}
		// The internal deadline bounds only the optimization. Concurrent ranking
		// retains unprobed targets, so a timeout with parsed candidates is still a
		// usable preload and the real transfer can validate them later.
	}()
	return preload
}

// Wait joins the background candidate phase. A canceled caller context wins
// immediately, while a completed preload returns its selection status.
func (p *CustomSpeedTestPreload) Wait(ctx context.Context) error {
	if p == nil || p.done == nil {
		return fmt.Errorf("custom speedtest preload is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-p.done:
		if err := ctx.Err(); err != nil {
			return err
		}
		return p.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *CustomSpeedTestPreload) prepared(ctx context.Context) (speedtest.Servers, *speedtest.Speedtest, bool, string, error) {
	if err := p.Wait(ctx); err != nil {
		return nil, nil, false, "", err
	}
	if len(p.targets) == 0 {
		return nil, nil, false, "", fmt.Errorf("no custom speedtest candidates available")
	}
	return p.targets, p.client, p.usedFallback, p.network, nil
}

// RunCustomSpeedTestContextTo consumes a completed preload through the
// speedtest-go implementation. It starts throughput only after candidate
// ranking has finished.
func (p *CustomSpeedTestPreload) RunCustomSpeedTestContextTo(ctx context.Context, writer io.Writer, num int, language string) error {
	targets, client, usedFallback, _, err := p.prepared(ctx)
	if err != nil {
		return err
	}
	if completed := customTargetsSpeedTestWithClientContextToWithPreloadedTargets(ctx, writer, targets, num, language, client, usedFallback); completed == 0 {
		return fmt.Errorf("no preloaded speedtest candidate completed throughput")
	}
	return nil
}

// RunOfficialCustomSpeedTestContextTo consumes a completed preload through
// the installed Ookla client when possible. Legacy direct-endpoint fallbacks
// remain on speedtest-go because the official client accepts only server IDs.
func (p *CustomSpeedTestPreload) RunOfficialCustomSpeedTestContextTo(ctx context.Context, writer io.Writer, num int, language string) error {
	targets, client, usedFallback, network, err := p.prepared(ctx)
	if err != nil {
		return err
	}
	if usedFallback {
		if completed := customTargetsSpeedTestWithClientContextToWithPreloadedTargets(ctx, writer, targets, num, language, client, true); completed == 0 {
			return fmt.Errorf("no preloaded speedtest candidate completed throughput")
		}
		return nil
	}
	if completed := officialTargetsSpeedTestContextToWithPreloadedTargets(ctx, writer, targets, num, language, network, false); completed == 0 {
		return fmt.Errorf("no preloaded speedtest candidate completed throughput")
	}
	return nil
}
