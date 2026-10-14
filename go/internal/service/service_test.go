package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTest starts the service behind an httptest server.
func newTest(t *testing.T, cfg Config) (*Service, *httptest.Server) {
	t.Helper()
	s, err := New(cfg, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts
}

// get fetches path and returns status, a header and the body.
func get(t *testing.T, url, header string) (int, string, string) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header.Get(header), string(b)
}

func TestWorkIsDeterministic(t *testing.T) {
	a, b, c := Work(7, 50, 4), Work(7, 50, 4), Work(8, 50, 4)
	if a != b {
		t.Fatal("same seed must give the same digest")
	}
	if a == c {
		t.Fatal("different seeds must give different digests")
	}
	if Work(1, 0, 0) == ([32]byte{}) {
		t.Fatal("zero work still hashes one block")
	}
}

func TestServesWork(t *testing.T) {
	_, ts := newTest(t, DefaultConfig())
	code, ctype, body := get(t, ts.URL+"/work", "Content-Type")
	if code != 200 || ctype != "application/json" || !regexp.MustCompile(`^\{"digest":"[0-9a-f]{16}"\}\n$`).MatchString(body) {
		t.Fatalf("got %d %q %q", code, ctype, body)
	}
	res, err := http.Post(ts.URL+"/work", "text/plain", strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST /work: %d, want 405", res.StatusCode)
	}
	if code, _, body := get(t, ts.URL+"/healthz", ""); code != 200 || body != "ok\n" {
		t.Fatalf("healthz: %d %q", code, body)
	}
}

func TestShedsWhenQueueIsFull(t *testing.T) {
	cfg := Config{Workers: 1, QueueLimit: 0, MaxWait: time.Second, WorkIters: 1, WorkKB: 1}
	s, ts := newTest(t, cfg)
	s.slots <- struct{}{} // occupy the only worker
	code, reason, _ := get(t, ts.URL+"/work", "X-Shed-Reason")
	if code != 503 || reason != "shed_queue_full" {
		t.Fatalf("got %d %q, want 503 shed_queue_full", code, reason)
	}
	iv := s.stats.Cut()
	if iv.Outcomes[ShedQueueFull] != 1 || iv.ShedLatency.Count() != 1 {
		t.Fatalf("outcomes %v", iv.Outcomes)
	}
}

func TestShedsAfterMaxWait(t *testing.T) {
	cfg := Config{Workers: 1, QueueLimit: 1, MaxWait: 30 * time.Millisecond, WorkIters: 1, WorkKB: 1}
	s, ts := newTest(t, cfg)
	s.slots <- struct{}{}
	start := time.Now()
	code, reason, _ := get(t, ts.URL+"/work", "X-Shed-Reason")
	if code != 503 || reason != "shed_wait_timeout" {
		t.Fatalf("got %d %q, want 503 shed_wait_timeout", code, reason)
	}
	if el := time.Since(start); el < 30*time.Millisecond {
		t.Fatalf("shed after %v, before the 30ms max wait", el)
	}
	if iv := s.stats.Cut(); iv.Outcomes[ShedWaitTimeout] != 1 || iv.QueueMax != 1 {
		t.Fatalf("outcomes %v queue max %d", iv.Outcomes, iv.QueueMax)
	}
}

func TestQueuedRequestIsServedWhenWorkerFrees(t *testing.T) {
	cfg := Config{Workers: 1, QueueLimit: 4, MaxWait: 2 * time.Second, WorkIters: 1, WorkKB: 1}
	s, ts := newTest(t, cfg)
	s.slots <- struct{}{}
	go func() {
		time.Sleep(40 * time.Millisecond)
		<-s.slots
	}()
	if code, _, _ := get(t, ts.URL+"/work", ""); code != 200 {
		t.Fatalf("got %d, want 200 once the worker frees", code)
	}
	iv := s.stats.Cut()
	if iv.Wait.Quantile(1) < 25*time.Millisecond || iv.Latency.Quantile(1) < 25*time.Millisecond {
		t.Fatalf("wait %v latency %v, want both >= 25ms (worker freed after 40ms)", iv.Wait.Quantile(1), iv.Latency.Quantile(1))
	}
	if iv.QueueSeconds < 0.03 {
		t.Fatalf("queue integral %.3f s, want about 0.04 s", iv.QueueSeconds)
	}
}

func TestConcurrentLoadAccountsForEveryRequest(t *testing.T) {
	cfg := Config{Workers: 2, QueueLimit: 8, MaxWait: 5 * time.Millisecond, WorkIters: 2000, WorkKB: 16}
	s, ts := newTest(t, cfg)
	const n = 200
	var wg sync.WaitGroup
	codes := make([]int, n)
	for i := range n {
		wg.Go(func() {
			res, err := http.Get(ts.URL + "/work")
			if err != nil {
				t.Error(err)
				return
			}
			_, _ = io.Copy(io.Discard, res.Body)
			res.Body.Close()
			codes[i] = res.StatusCode
		})
	}
	wg.Wait()
	iv := s.stats.Cut()
	var ok, shed uint64
	for _, c := range codes {
		switch c {
		case 200:
			ok++
		case 503:
			shed++
		default:
			t.Fatalf("unexpected status %d", c)
		}
	}
	if iv.Outcomes[OK] != ok || iv.Outcomes[ShedQueueFull]+iv.Outcomes[ShedWaitTimeout] != shed {
		t.Fatalf("server counted %v, clients saw ok=%d shed=%d", iv.Outcomes, ok, shed)
	}
	if iv.QueueMax > cfg.QueueLimit {
		t.Fatalf("queue reached %d, limit %d", iv.QueueMax, cfg.QueueLimit)
	}
	if busy, waiting, _ := s.stats.Gauges(); busy != 0 || waiting != 0 {
		t.Fatalf("after the burst busy=%d waiting=%d, want 0 0", busy, waiting)
	}
	if iv.BusySeconds > iv.Duration.Seconds()*float64(cfg.Workers)+1e-6 {
		t.Fatalf("busy integral %.3f exceeds %d workers x %.3f s", iv.BusySeconds, cfg.Workers, iv.Duration.Seconds())
	}
}

func TestStatsIntegralsWithFakeClock(t *testing.T) {
	now := time.Unix(0, 0)
	clock := func() time.Time { return now }
	st := NewStats(clock)
	st.startDirect()
	st.startDirect()
	now = now.Add(time.Second) // 2 busy for 1 s
	if !st.tryEnqueue(10) {
		t.Fatal("enqueue refused")
	}
	now = now.Add(500 * time.Millisecond) // 2 busy + 1 waiting for 0.5 s
	st.done(OK, time.Second, true)
	st.dequeue(true, 500*time.Millisecond)
	now = now.Add(500 * time.Millisecond) // 2 busy for 0.5 s
	iv := st.Cut()
	if iv.Duration != 2*time.Second || iv.BusySeconds != 4 || iv.QueueSeconds != 0.5 || iv.QueueMax != 1 {
		t.Fatalf("got duration %v busy %.2f queue %.2f max %d", iv.Duration, iv.BusySeconds, iv.QueueSeconds, iv.QueueMax)
	}
	if st.FirstRequest() != time.Unix(0, 0) {
		t.Fatal("first request time not recorded")
	}
	if iv2 := st.Cut(); iv2.BusySeconds != 0 || iv2.Outcomes[OK] != 0 {
		t.Fatal("Cut must reset the interval")
	}
}

func TestMetricsEndpoint(t *testing.T) {
	_, ts := newTest(t, DefaultConfig())
	get(t, ts.URL+"/work", "")
	code, _, body := get(t, ts.URL+"/metrics", "")
	for _, want := range []string{`caplab_requests_total{outcome="ok"} 1`, "caplab_workers 4", "caplab_queue_limit 64", "caplab_workers_busy 0"} {
		if code != 200 || !strings.Contains(body, want+"\n") {
			t.Fatalf("metrics missing %q:\n%s", want, body)
		}
	}
}

func TestConfigValidate(t *testing.T) {
	good := DefaultConfig()
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := []func(*Config){
		func(c *Config) { c.Workers = 0 },
		func(c *Config) { c.QueueLimit = -1 },
		func(c *Config) { c.MaxWait = 0 },
		func(c *Config) { c.WorkIters = 1_000_000 },
		func(c *Config) { c.WorkKB = 1 << 20 },
	}
	for i, mutate := range bad {
		c := good
		mutate(&c)
		if _, err := New(c, time.Now); err == nil {
			t.Fatalf("case %d: invalid config accepted", i)
		}
	}
}

func TestCheckLoopback(t *testing.T) {
	for _, ok := range []string{"127.0.0.1:0", "127.0.0.1:8080", "[::1]:9000", "localhost:80"} {
		if err := CheckLoopback(ok); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	for _, bad := range []string{":8080", "0.0.0.0:8080", "[::]:8080", "192.168.1.5:80", "example.com:80", "127.0.0.1", "127.0.0.1:http-alt-x"} {
		if err := CheckLoopback(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestServerSpeaksHTTP1Only(t *testing.T) {
	srv := NewServer(http.NotFoundHandler())
	if srv.Protocols == nil || !srv.Protocols.HTTP1() || srv.Protocols.HTTP2() || srv.Protocols.UnencryptedHTTP2() {
		t.Fatalf("protocols %v, want HTTP/1 only", srv.Protocols)
	}
	if srv.ReadHeaderTimeout == 0 || srv.MaxHeaderBytes == 0 {
		t.Fatal("timeouts and header limits must be set")
	}
}
