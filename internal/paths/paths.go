// Package paths holds the rules for touching the file system on a
// document's behalf. Documents are untrusted: a path one of them leads to
// must pass CheckPath before anything opens it, and is read with
// ReadRegular.
package paths

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Checks such as "inside the open folder" compare the resolved path
// CheckPath returns, never the path's text, which a link inside the folder
// could point anywhere.

// maxLinkHops bounds how many symbolic links CheckPath follows.
const maxLinkHops = 32

// CheckPath reports whether p, a path a document points to, is safe to
// touch. current is the document's own path. p must stay on a drive letter
// or on current's network share, also after following symbolic links and
// junctions, which are resolved one path element at a time without touching
// their targets. Device names (CON, COM1…) and alternate data streams
// (file:stream) are refused. It returns p with every link resolved.
func CheckPath(p, current string) (string, error) {
	for hops := 0; ; hops++ {
		if err := checkText(p, current); err != nil {
			return "", err
		}
		next, linked, err := firstLink(p, current)
		if err != nil {
			return "", err
		}
		if !linked {
			return p, nil
		}
		if hops == maxLinkHops {
			return "", fmt.Errorf("%w: too many links in %s", ErrUnsupported, p)
		}
		p = next
	}
}

// Real resolves the links in p, a path the user chose (the open folder),
// checked like a document path on p's own volume.
func Real(p string) (string, error) { return CheckPath(p, p) }

// checkText applies the rules that need only the path's text.
func checkText(p, current string) error {
	// Touching \\server\share makes Windows authenticate to that server,
	// leaking the user's NTLM hash to whoever wrote the document.
	if share := uncShare(p); share != "" && !isLocalVolume(share) && !strings.EqualFold(share, uncShare(current)) {
		return fmt.Errorf("%w: network path %s", ErrUnsupported, share)
	}
	vol := filepath.VolumeName(p)
	if vol != "" && !isLocalVolume(vol) && !strings.EqualFold(vol, filepath.VolumeName(current)) {
		return fmt.Errorf("%w: network path %s", ErrUnsupported, vol)
	}
	rest := p[len(vol):]
	if strings.ContainsRune(rest, ':') {
		return fmt.Errorf("%w: %s", ErrUnsupported, p)
	}
	for _, elem := range strings.FieldsFunc(rest, isSeparator) {
		if isDeviceName(elem) {
			return fmt.Errorf("%w: device name %s", ErrUnsupported, elem)
		}
	}
	return nil
}

// firstLink walks p from its root and, at the first element that is a link,
// returns p with that element replaced by the link's target. The target's
// own text is checked first, since joining paths can rewrite it.
func firstLink(p, current string) (next string, linked bool, err error) {
	vol := filepath.VolumeName(p)
	elems := strings.FieldsFunc(p[len(vol):], isSeparator)
	prefix := vol
	if len(p) > len(vol) && isSeparator(rune(p[len(vol)])) {
		prefix += string(filepath.Separator)
	}
	for i, elem := range elems {
		prefix = filepath.Join(prefix, elem)
		fi, err := os.Lstat(prefix)
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil // nothing further to follow; opening it fails later
		}
		if err != nil {
			return "", false, err
		}
		if fi.Mode()&(fs.ModeSymlink|fs.ModeIrregular) == 0 {
			continue
		}
		target, err := os.Readlink(prefix)
		if err != nil {
			if fi.Mode()&fs.ModeSymlink == 0 {
				continue // a reparse point that isn't a link, e.g. a cloud file
			}
			return "", false, err
		}
		if err := checkText(target, current); err != nil {
			return "", false, err
		}
		switch {
		case filepath.IsAbs(target), filepath.VolumeName(target) != "":
		case strings.HasPrefix(target, string(filepath.Separator)):
			target = vol + target // root of the link's drive
		default:
			target = filepath.Join(filepath.Dir(prefix), target)
		}
		return filepath.Join(append([]string{target}, elems[i+1:]...)...), true, nil
	}
	return "", false, nil
}

func isSeparator(r rune) bool { return r == '\\' || r == '/' }

// isDeviceName reports whether elem names a DOS device, which Windows opens
// in any folder and with any extension (COM1.png is the serial port).
func isDeviceName(elem string) bool {
	base, _, _ := strings.Cut(elem, ".")
	base = strings.ToUpper(strings.TrimRight(base, " "))
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	if num, ok := strings.CutPrefix(base, "COM"); ok {
		return isPortNumber(num)
	}
	if num, ok := strings.CutPrefix(base, "LPT"); ok {
		return isPortNumber(num)
	}
	return false
}

// isPortNumber matches the digits Windows accepts after COM and LPT,
// including the superscripts ¹ ² ³.
func isPortNumber(s string) bool {
	switch s {
	case "0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "¹", "²", "³":
		return true
	}
	return false
}

// ReadRegular reads the regular file at p, up to limit bytes.
func ReadRegular(p string, limit int64) ([]byte, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: not a regular file: %s", ErrUnsupported, p)
	}
	if fi.Size() > limit {
		return nil, fmt.Errorf("%s is too large (%d KB, limit %d KB)", filepath.Base(p), fi.Size()>>10, limit>>10)
	}
	f, err := os.Open(p) //nolint:gosec // G304: p passed CheckPath, or the user chose it
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: not a regular file: %s", ErrUnsupported, p)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is too large", filepath.Base(p))
	}
	return data, nil
}

// SamePath reports whether a and b name the same path, ignoring case on
// Windows, whose file systems do.
func SamePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// Within reports whether path is root or inside it.
func Within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// ErrUnsupported is returned for paths and links the viewer refuses: a
// network path, a device, a scheme such as javascript:, and so on.
var ErrUnsupported = errors.New("unsupported path")

var markdownExts = map[string]bool{".md": true, ".markdown": true, ".mdown": true, ".mkd": true, ".mkdn": true}

// IsMarkdown reports whether path has a Markdown file extension.
func IsMarkdown(path string) bool {
	return markdownExts[strings.ToLower(filepath.Ext(path))]
}

// uncShare returns the \\server\share prefix of a network path (either slash
// style, including \\?\UNC\ paths), or "" for any other path.
func uncShare(p string) string {
	isSep := func(c byte) bool { return c == '\\' || c == '/' }
	// \??\ is the NT object namespace, which reaches UNC\ and devices too.
	if len(p) >= 4 && isSep(p[0]) && p[1] == '?' && p[2] == '?' && isSep(p[3]) {
		return `\??\`
	}
	if len(p) < 2 || !isSep(p[0]) || !isSep(p[1]) {
		return ""
	}
	parts := strings.FieldsFunc(p[2:], func(r rune) bool { return r == '\\' || r == '/' })
	if len(parts) >= 2 && (parts[0] == "?" || parts[0] == ".") {
		if strings.EqualFold(parts[1], "UNC") {
			parts = parts[2:]
		} else {
			return `\\` + parts[0] + `\` + parts[1] // device path, e.g. \\?\C:
		}
	}
	switch len(parts) {
	case 0:
		return `\\`
	case 1:
		return `\\` + parts[0]
	}
	return `\\` + parts[0] + `\` + parts[1]
}

// isDrive reports whether vol is a plain drive letter such as "C:".
func isDrive(vol string) bool {
	return len(vol) == 2 && vol[1] == ':' && (vol[0]|0x20 >= 'a' && vol[0]|0x20 <= 'z')
}

// isLocalVolume reports whether vol is a local volume: a drive letter, the
// same as a \\?\ device path (\\?\C:), or a volume GUID (\\?\Volume{…}),
// which is where a junction to a mounted volume points.
func isLocalVolume(vol string) bool {
	if isDrive(vol) {
		return true
	}
	if len(vol) < 4 || !isSeparator(rune(vol[0])) || !isSeparator(rune(vol[1])) || vol[2] != '?' || !isSeparator(rune(vol[3])) {
		return false
	}
	rest := vol[4:]
	return isDrive(rest) || (len(rest) > 8 && strings.EqualFold(rest[:7], "Volume{") && !strings.ContainsAny(rest, `\/`))
}

// InsideFolder reports whether the resolved path real is inside folder, also
// resolved.
func InsideFolder(folder, real string) bool {
	if folder == "" {
		return false
	}
	f, err := Real(folder)
	return err == nil && Within(f, real)
}

// CheckRaw judges a link's own text, before it is joined to any folder:
// joining can rewrite a network path (on Linux, //server/share becomes
// /server/share). It refuses only network paths; CheckPath judges the rest
// once the final path is known.
func CheckRaw(p, current string) error {
	if err := checkText(p, current); err != nil && uncShare(p) != "" {
		return err
	}
	return nil
}
