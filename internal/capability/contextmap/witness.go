package contextmap

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

// Verdict is what check can honestly say about one source.
type Verdict string

const (
	VerdictOK            Verdict = "OK"      // witnessed, and unchanged
	VerdictDrift         Verdict = "DRIFT"   // witnessed, and the content changed
	VerdictGone          Verdict = "GONE"    // the subject moved or was deleted
	VerdictSkipped       Verdict = "SKIPPED" // could not witness right now (offline)
	VerdictUnwitnessable Verdict = "AGE"     // external: drift is undetectable; age is all we have
)

// Resolver witnesses sources against the real world. Repo is the git repo root;
// HTTP, when nil, disables URL fetching so check degrades to SKIPPED rather
// than failing.
type Resolver struct {
	Repo string
	HTTP *http.Client
}

// errGone marks a subject that is no longer there.
var errGone = errors.New("subject gone")

// Witness returns the current evidence for a source: a git object id, a content
// hash, or an HTTP body hash. External sources have no local witness and return
// "" -- their freshness is judged by age alone.
func (r *Resolver) Witness(src Source) (string, error) {
	switch src.Kind {
	case KindGit:
		return r.gitObject(src.Locator)
	case KindFile:
		b, err := os.ReadFile(src.Locator)
		if err != nil {
			if os.IsNotExist(err) {
				return "", errGone
			}
			return "", fmt.Errorf("read %s: %w", src.Locator, err)
		}
		return hashBytes(b), nil
	case KindURL:
		if r.HTTP == nil {
			return "", nil // caller maps this to SKIPPED
		}
		resp, err := r.HTTP.Get(src.Locator)
		if err != nil {
			return "", nil // offline: SKIPPED, not a failure
		}
		defer resp.Body.Close()
		if etag := resp.Header.Get("ETag"); etag != "" {
			return etag, nil
		}
		var sb strings.Builder
		h := sha256.New()
		if _, err := copyTo(h, resp.Body); err != nil {
			return "", nil
		}
		sb.WriteString(hex.EncodeToString(h.Sum(nil)))
		return sb.String(), nil
	case KindExternal:
		return "", nil
	}
	return "", fmt.Errorf("witness: unknown kind %q", src.Kind)
}

// Compare witnesses a source now and judges it against what was recorded.
func (r *Resolver) Compare(src Source, recorded string) (Verdict, error) {
	if !src.Provable() {
		return VerdictUnwitnessable, nil
	}
	now, err := r.Witness(src)
	if err != nil {
		if errors.Is(err, errGone) {
			return VerdictGone, nil
		}
		return "", err
	}
	if now == "" {
		return VerdictSkipped, nil // could not witness (e.g. offline URL)
	}
	if now == recorded {
		return VerdictOK, nil
	}
	return VerdictDrift, nil
}

// Head returns the current HEAD commit, or "" outside a git repo.
func (r *Resolver) Head() (string, error) {
	out, err := r.git("rev-parse", "HEAD")
	if err != nil {
		return "", nil
	}
	return out, nil
}

// ChangedSince lists repo-relative paths that changed between rev and HEAD.
func (r *Resolver) ChangedSince(rev string) ([]string, error) {
	if rev == "" {
		return nil, nil
	}
	out, err := r.git("diff", "--name-only", rev+"..HEAD")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// gitObject returns the object id of the blob or tree at path in HEAD. One call
// witnesses a file and a directory alike: the id changes exactly when the
// content under that path changes. The repo root (".") is witnessed via the
// root tree of HEAD, since `git rev-parse HEAD:.` is rejected by git.
func (r *Resolver) gitObject(path string) (string, error) {
	if path == "." {
		out, err := r.git("rev-parse", "HEAD^{tree}")
		if err != nil {
			return "", errGone
		}
		return out, nil
	}
	out, err := r.git("rev-parse", "HEAD:"+path)
	if err != nil {
		return "", errGone // git exits non-zero when the path is not in HEAD
	}
	return out, nil
}

func (r *Resolver) git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if r.Repo != "" {
		cmd.Dir = r.Repo
	}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// copyTo is io.Copy, named locally to keep the import list small and explicit.
func copyTo(dst io.Writer, src io.Reader) (int64, error) { return io.Copy(dst, src) }
