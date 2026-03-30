package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/we-be/shoal/internal/api"
)

// executeActionWithRetry wraps executeAction with retry logic.
// DOM timing issues (element exists but isn't interactive) usually resolve
// within 1s. Retrying avoids failing the entire request for transient issues.
func executeActionWithRetry(ctx context.Context, action api.Action, maxAttempts int) error {
	// Don't retry wait/eval actions — they have their own timeout logic
	if action.Type == "wait" || action.Type == "wait_for" || action.Type == "eval" {
		return executeAction(ctx, action)
	}

	var lastErr error
	for attempt := range maxAttempts {
		lastErr = executeAction(ctx, action)
		if lastErr == nil {
			return nil
		}
		if attempt < maxAttempts-1 {
			time.Sleep(500 * time.Millisecond)
		}
	}
	return lastErr
}

// executeAction runs a single browser automation step via JS evaluation.
// Uses Runtime.evaluate for broad CDP compatibility (works with Lightpanda,
// Chrome, and anything else that speaks the protocol).
func executeAction(ctx context.Context, action api.Action) error {
	switch action.Type {
	case "fill":
		// Set value AND dispatch input/change events so framework-bound
		// forms (Angular, React, PingFederate SSO) detect the change.
		js := fmt.Sprintf(`(() => {
			const el = document.querySelector(%q);
			if (!el) throw new Error('selector not found: ' + %q);
			el.focus();
			el.value = '';
			el.value = %q;
			el.dispatchEvent(new Event('input', { bubbles: true }));
			el.dispatchEvent(new Event('change', { bubbles: true }));
		})()`, action.Selector, action.Selector, action.Value,
		)
		return chromedp.Run(ctx, chromedp.Evaluate(js, nil))

	case "click":
		js := fmt.Sprintf(`document.querySelector(%q).click()`, action.Selector)
		err := chromedp.Run(ctx, chromedp.Evaluate(js, nil))
		if err != nil {
			return err
		}
		return chromedp.Run(ctx, chromedp.WaitReady("body", chromedp.ByQuery))

	case "submit":
		js := fmt.Sprintf(`document.querySelector(%q).submit()`, action.Selector)
		err := chromedp.Run(ctx, chromedp.Evaluate(js, nil))
		if err != nil {
			return err
		}
		return chromedp.Run(ctx, chromedp.WaitReady("body", chromedp.ByQuery))

	case "wait":
		if action.WaitMS > 0 {
			return chromedp.Run(ctx, chromedp.Sleep(time.Duration(action.WaitMS)*time.Millisecond))
		}
		js := fmt.Sprintf(
			`new Promise(r => { const check = () => document.querySelector(%q) ? r() : setTimeout(check, 100); check(); })`,
			action.Selector,
		)
		return chromedp.Run(ctx, chromedp.Evaluate(js, nil))

	case "wait_for":
		waitMS := action.WaitMS
		if waitMS == 0 {
			waitMS = 10000
		}
		js := fmt.Sprintf(
			`new Promise((resolve, reject) => {
				const deadline = Date.now() + %d;
				const check = () => {
					if (document.querySelector(%q)) return resolve(true);
					if (Date.now() > deadline) return reject(new Error('wait_for timeout: ' + %q));
					setTimeout(check, 100);
				};
				check();
			})`, waitMS, action.Selector, action.Selector,
		)
		return chromedp.Run(ctx, chromedp.Evaluate(js, nil))

	case "eval":
		return chromedp.Run(ctx, chromedp.Evaluate(action.Value, nil))

	default:
		return fmt.Errorf("unknown action type: %s", action.Type)
	}
}

// waitForNetworkIdle waits until no network requests have fired for the given
// quiet period. Catches JS-rendered content that loads after DOMContentLoaded.
func waitForNetworkIdle(ctx context.Context, quiet time.Duration) error {
	js := fmt.Sprintf(`new Promise(resolve => {
		let timer;
		const reset = () => {
			clearTimeout(timer);
			timer = setTimeout(resolve, %d);
		};
		const orig = window.fetch;
		window.fetch = function() { reset(); return orig.apply(this, arguments); };
		const origXHR = XMLHttpRequest.prototype.send;
		XMLHttpRequest.prototype.send = function() { reset(); return origXHR.apply(this, arguments); };
		reset();
	})`, quiet.Milliseconds())

	return chromedp.Run(ctx, chromedp.Evaluate(js, nil))
}

// waitForCFChallenge detects if we landed on a Cloudflare challenge page
// and waits for the browser to solve it (title changes when solved).
func waitForCFChallenge(ctx context.Context) error {
	var title string
	if err := chromedp.Run(ctx, chromedp.Title(&title)); err != nil {
		return err
	}

	cfTitles := []string{"Just a moment...", "DDoS-GUARD", "Attention Required"}
	isChallenge := false
	for _, ct := range cfTitles {
		if title == ct {
			isChallenge = true
			break
		}
	}
	if !isChallenge {
		return nil
	}

	log.Printf("CF challenge detected (%q), waiting for resolution...", title)

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("challenge timeout: %w", ctx.Err())
		default:
		}

		time.Sleep(1 * time.Second)

		var current string
		if err := chromedp.Run(ctx, chromedp.Title(&current)); err != nil {
			return err
		}

		if current != title {
			log.Printf("CF challenge resolved: %q -> %q", title, current)
			chromedp.Run(ctx, chromedp.WaitReady("body", chromedp.ByQuery))
			return nil
		}
	}
}
