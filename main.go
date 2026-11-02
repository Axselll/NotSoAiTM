package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"
)

type state struct {
	Origin         string            `json:"origin"`
	Cookies        []*network.Cookie `json:"cookies"`
	LocalStorage   map[string]string `json:"localStorage"`
	SessionStorage map[string]string `json:"sessionStorage"`
}

func main() {
	mode := flag.String("mode", "", "save | restore")
	target := flag.String("url", "", "site URL, e.g. https://example.com/")
	file := flag.String("state", "state.json", "state file path")
	profile := flag.String("profile", "", "persistent browser profile dir (optional)")
	cookie := flag.String("cookie", "", "auth cookie name to require on save (optional)")
	timeout := flag.Duration("timeout", 5*time.Minute, "overall timeout")
	flag.Parse()

	u, err := url.Parse(*target)
	if err != nil || u.Scheme == "" || u.Host == "" {
		log.Fatalf("-url must be absolute, got %q", *target)
	}
	origin := u.Scheme + "://" + u.Host

	run := map[string]func(context.Context, opts) error{
		"save":    save,
		"restore": restore,
	}[*mode]
	if run == nil {
		log.Fatal("-mode must be save or restore")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if err := run(ctx, opts{*target, origin, *file, *profile, *cookie}); err != nil {
		log.Fatal(err)
	}
}

type opts struct {
	target, origin, path, profile, cookie string
}

func save(parent context.Context, o opts) error {
	ctx, cancel := browser(parent, "")
	defer cancel()

	if err := chromedp.Do(ctx, chromedp.Navigate(o.target)); err != nil {
		return fmt.Errorf("open %s: %w", o.target, err)
	}
	fmt.Print("→ Log in, then press Enter… ")
	bufio.NewReader(os.Stdin).ReadString('\n')

	if err := chromedp.Do(ctx, chromedp.Navigate(o.target)); err != nil {
		return fmt.Errorf("reopen %s: %w", o.target, err)
	}

	res, err := chromedp.Call(ctx, network.GetCookies, network.GetCookiesParams{URLs: []string{o.origin, o.target}})
	if err != nil {
		return fmt.Errorf("read cookies: %w", err)
	}
	if len(res.Cookies) == 0 {
		return errors.New("no cookies captured — did the login finish before you pressed Enter?")
	}
	if o.cookie != "" && !hasCookie(res.Cookies, o.cookie) {
		return fmt.Errorf("auth cookie %q not found — log in fully before pressing Enter", o.cookie)
	}
	local, err := chromedp.Run(ctx, chromedp.Evaluate[map[string]string](jsDump("localStorage")))
	if err != nil {
		return fmt.Errorf("read localStorage: %w", err)
	}
	session, err := chromedp.Run(ctx, chromedp.Evaluate[map[string]string](jsDump("sessionStorage")))
	if err != nil {
		return fmt.Errorf("read sessionStorage: %w", err)
	}

	data, err := json.MarshalIndent(state{o.origin, res.Cookies, local, session}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	if err := os.WriteFile(o.path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", o.path, err)
	}
	fmt.Printf("✓ saved %d cookies → %s\n", len(res.Cookies), o.path)
	return nil
}

func hasCookie(cookies []*network.Cookie, name string) bool {
	for _, c := range cookies {
		if c.Name == name {
			return true
		}
	}
	return false
}

func restore(parent context.Context, o opts) error {
	data, err := os.ReadFile(o.path)
	if err != nil {
		return fmt.Errorf("read %s: %w", o.path, err)
	}
	var s state
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("decode %s: %w", o.path, err)
	}

	ctx, cancel := browser(parent, o.profile)
	defer cancel()

	if err := chromedp.Do(ctx, chromedp.Navigate("about:blank")); err != nil {
		return fmt.Errorf("start browser: %w", err)
	}

	params := make([]*network.CookieParam, 0, len(s.Cookies))
	for _, c := range s.Cookies {
		p := &network.CookieParam{
			Name: c.Name, Value: c.Value, URL: s.Origin,
			Path: c.Path, Secure: c.Secure, HTTPOnly: c.HTTPOnly, SameSite: c.SameSite,
		}
		if !c.Session {
			p.Expires = cdp.TimeSinceEpoch(c.Expires)
		}
		params = append(params, p)
	}
	if _, err := chromedp.Call(ctx, network.SetCookies, network.SetCookiesParams{Cookies: params}); err != nil {
		return fmt.Errorf("set cookies: %w", err)
	}

	js, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("encode script: %w", err)
	}
	source := `(()=>{const s=` + string(js) + `;if(location.origin!==s.origin)return;` +
		`for(const[k,v]of Object.entries(s.localStorage))localStorage.setItem(k,v);` +
		`for(const[k,v]of Object.entries(s.sessionStorage))sessionStorage.getItem(k)===null&&sessionStorage.setItem(k,v);})();`
	if _, err := chromedp.Call(ctx, page.AddScriptToEvaluateOnNewDocument, page.AddScriptToEvaluateOnNewDocumentParams{Source: source}); err != nil {
		return fmt.Errorf("add script: %w", err)
	}

	if err := chromedp.Do(ctx, chromedp.Navigate(o.target)); err != nil {
		return fmt.Errorf("open %s: %w", o.target, err)
	}
	_, dir := chromedp.KeptOpen(ctx)
	fmt.Printf("✓ session restored — window left open\n  profile: %s\n", dir)
	return nil
}

func browser(parent context.Context, profile string) (context.Context, context.CancelFunc) {
	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", false),
	)
	if profile != "" {
		abs, _ := filepath.Abs(profile)
		allocOpts = append(allocOpts, chromedp.UserDataDir(abs))
	}
	allocCtx, allocCancel := chromedp.NewExecAllocator(parent, allocOpts...)
	ctx, ctxCancel := chromedp.NewContext(allocCtx, remote.WithKeepOpen())
	return ctx, func() { ctxCancel(); allocCancel() }
}

func jsDump(store string) string {
	return "Object.fromEntries(Object.entries(" + store + "))"
}
