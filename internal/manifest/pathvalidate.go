package manifest

import "strings"

// ValidateEntryPath enforces FR-003 path hygiene: a manifest entry path must be
// a forward-slash-separated relative path of plain segments. Validation is
// purely lexical (no filesystem access, no symlink resolution) and applies to
// verified manifests too (defense in depth). Rejections return
// ManifestInvalidPathError naming the offending path and form.
//
// Check order matters: the drive-letter / UNC prefix forms (which also contain
// a colon or backslash) are detected before the generic backslash check so
// they report the more specific reason.
func ValidateEntryPath(p string) error {
	reject := func(reason string) error {
		return &ManifestInvalidPathError{Path: p, Reason: reason}
	}

	if strings.HasPrefix(p, `\\`) {
		return reject("drive-letter/UNC prefix")
	}
	if len(p) >= 2 && p[1] == ':' && isASCIILetter(p[0]) {
		return reject("drive-letter/UNC prefix")
	}
	if strings.HasPrefix(p, "/") {
		return reject("leading separator")
	}
	if strings.Contains(p, `\`) {
		return reject("contains backslash")
	}
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "":
			return reject("empty segment")
		case "..":
			return reject(`contains ".." segment`)
		case ".":
			return reject(`contains "." segment`)
		}
	}
	return nil
}

func isASCIILetter(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}
