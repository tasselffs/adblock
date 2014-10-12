package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/pmezard/adblock/adblock"
)

var (
	listen = flag.String("listen", "localhost:1080", "listen on address")
	logp   = flag.Bool("log", false, "enable logging")
)

type FilteringHandler struct {
	Matcher adblock.Matcher
	Rules   []string
}

func logRequest(r *http.Request) {
	log.Printf("%s %s %s %s\n", r.Proto, r.Method, r.URL, r.Host)
	buf := &bytes.Buffer{}
	r.Header.Write(buf)
	log.Println(string(buf.Bytes()))
}

func (h *FilteringHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	if *logp {
		logRequest(r)
	}

	client := &http.Client{}
	r.RequestURI = ""
	if len(r.URL.Scheme) > 0 {
		r.URL.Scheme = strings.Map(unicode.ToLower, r.URL.Scheme)
	} else {
		r.URL.Scheme = "http"
	}
	if len(r.URL.Host) == 0 {
		r.URL.Host = r.Host
	}

	start := time.Now()
	matched, id := h.Matcher(r.URL.String(), r.URL.Host)
	end := time.Now()
	duration := end.Sub(start) / time.Millisecond
	if matched {
		rule := h.Rules[id]
		log.Printf("rejected in %dms: %s\n", duration, r.URL.String())
		log.Printf("  by %s\n", rule)
		w.WriteHeader(404)
		return
	} else {
		log.Printf("accepted in %dms: %s\n", duration, r.URL.String())
	}

	if r.Method == "HEAD" {
		r.Header.Del("Accept-Encoding")
	}
	r.Close = true

	resp, err := client.Do(r)
	if err != nil && err != io.EOF {
		log.Printf("error: %s\n", err)
		if !*logp {
			logRequest(r)
		}
		return
	}
	headers := w.Header()
	for k, v := range resp.Header {
		headers[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	_, err = io.Copy(w, resp.Body)
	resp.Body.Close()
}

func loadBlackList(path string, matcher *adblock.RuleMatcher,
	rules []string) ([]string, int, error) {

	fp, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer fp.Close()

	read := 0
	scanner := bufio.NewScanner(fp)
	for scanner.Scan() {
		s := scanner.Text()
		rule, err := adblock.ParseRule(s)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: could not parse rule:\n  %s\n  %s\n",
				scanner.Text(), err)
			continue
		}
		if rule == nil {
			continue
		}
		err = matcher.AddRule(rule, len(rules))
		read += 1
		if err == nil {
			rules = append(rules, s)
		}
	}
	return rules, read, scanner.Err()
}

func loadBlackLists(paths []string) (adblock.Matcher, []string, error) {
	log.Printf("reading black lists\n")
	matcher := adblock.NewMatcher()
	read := 0
	rules := []string{}
	for _, path := range paths {
		updated, r, err := loadBlackList(path, matcher, rules)
		rules = updated
		if err != nil {
			return nil, nil, err
		}
		read += r
	}
	log.Printf("blacklists built: %d / %d added\n", len(rules), read)
	return func(url, domain string) (bool, int) {
		return matcher.Match(url, domain)
	}, rules, nil
}

func runProxy() error {
	flag.Parse()
	matcher, rules, err := loadBlackLists(flag.Args())
	if err != nil {
		return err
	}
	h := &FilteringHandler{Matcher: matcher, Rules: rules}
	return http.ListenAndServe(*listen, h)
}

func main() {
	err := runProxy()
	if err != nil {
		log.Fatalf("error: %s\n", err)
	}
}
