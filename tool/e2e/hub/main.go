// Command hub is a minimal git smart-HTTP server. It exists only so the multi-site
// end-to-end test can put a real git remote in a container: alpine's git has no
// git-daemon, and the test needs a transport, not an authentication scheme.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	root := flag.String("root", "/srv", "the directory of bare repositories")
	listen := flag.String("listen", "0.0.0.0:8080", "the address to listen on")
	flag.Parse()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		serve(w, r, *root)
	})
	log.Fatal(http.ListenAndServe(*listen, nil))
}

func serve(w http.ResponseWriter, r *http.Request, root string) {
	log.Printf("%s %s", r.Method, r.URL.String())
	repo, rest, ok := split(strings.TrimPrefix(r.URL.Path, "/"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	dir := filepath.Join(root, repo)
	if _, err := os.Stat(dir); err != nil {
		http.NotFound(w, r)
		return
	}

	switch {
	case rest == "info/refs" && r.Method == http.MethodGet:
		service := r.URL.Query().Get("service")
		if service != "git-upload-pack" && service != "git-receive-pack" {
			http.Error(w, "unknown service", http.StatusBadRequest)
			return
		}
		sub := strings.TrimPrefix(service, "git-")
		out, err := exec.Command("git", sub, "--stateless-rpc", "--advertise-refs", dir).Output()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		line := "# service=" + service + "\n"
		w.Header().Set("Content-Type", "application/x-"+service+"-advertisement")
		fmt.Fprintf(w, "%04x%s0000", len(line)+4, line)
		_, _ = w.Write(out)
	case (rest == "git-upload-pack" || rest == "git-receive-pack") && r.Method == http.MethodPost:
		service := rest
		w.Header().Set("Content-Type", "application/x-"+service+"-result")
		cmd := exec.Command("git", strings.TrimPrefix(service, "git-"), "--stateless-rpc", dir)
		cmd.Stdin = r.Body
		cmd.Stdout = w
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Printf("%s: %v", service, err)
		}
	default:
		http.NotFound(w, r)
	}
}

// split returns the repository name and the path under it.
func split(path string) (repo, rest string, ok bool) {
	i := strings.Index(path, ".git/")
	if i < 0 {
		return "", "", false
	}
	return path[:i+4], path[i+5:], true
}
