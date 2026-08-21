// Package atomicfile writes a file so a concurrent reader never sees it
// half-written.
//
// The engine watches the files Forge saves with fsnotify and reloads them the
// moment they change. A plain os.WriteFile truncates and then writes, leaving a
// window — small, but hit reliably by a watcher that fires on the first write
// event — where the file on disk is empty or half a JSON document. The engine
// reads it, fails to parse, and logs an error for a file that was fine a
// millisecond earlier and fine a millisecond later.
package atomicfile

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/natefinch/atomic"
)

// Write replaces the file at path with data, atomically.
//
// It delegates to github.com/natefinch/atomic, which was already an indirect
// dependency and does exactly what is needed: the temp file is created in the
// target's own directory, so the rename stays within one filesystem and is
// therefore atomic; it is fsynced before the rename, so a crash cannot leave a
// zero-length file in place; the original's mode is carried over; and the temp
// is removed if any step fails. Writing that again here would be the same code
// with fewer eyes on it.
//
// Three things it deliberately does not do:
//
// It does not fsync the containing directory after the rename, so the rename
// itself is durable only once the filesystem flushes. That matters for
// surviving a power cut, not for surviving a concurrent reader, and an
// authoring tool that loses the last save in a crash is an acceptable trade
// against a portability wart.
//
// It does not follow symlinks or preserve hard links: rename replaces the path,
// so a symlinked schema.json becomes a regular file and the original target
// keeps the old content. Inherent to rename-based atomic writes rather than a
// choice, but worth knowing before someone symlinks a shared schema between
// projects and wonders why the engine never sees their edits.
//
// It does not set a mode on a file it creates — see below.
func Write(path string, data []byte) error {
	// The engine's watcher filters on filepath.Ext(name) == ".json", and the
	// temp files are named "<base><random>", so "goblin.json" yields
	// "goblin.json1234567" with extension ".json1234567" — invisible to it.
	//
	// With one exception, which is why this guard exists: the base name is
	// passed straight to os.CreateTemp as a *pattern*, and the last '*' in a
	// pattern is where the random digits go. A file called "ma*chine.json"
	// produces "ma1273057549chine.json", whose extension really is ".json" —
	// so the watcher would try to load a machine that then vanishes. '*' is a
	// legal filename character, and these names come from the user's project.
	if strings.ContainsRune(filepath.Base(path), '*') {
		return fmt.Errorf(
			"atomicfile: %s: '*' in a filename would make the temporary file visible to the engine's watcher", path)
	}

	// Whether the file already exists decides its mode afterwards. The
	// dependency copies the mode of an existing destination, but a file it
	// creates keeps the temp file's 0600 — which would leave a new schema.json
	// unreadable by a game process running as another user.
	_, statErr := os.Stat(path)
	creating := os.IsNotExist(statErr)

	if err := atomic.WriteFile(path, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("atomicfile: writing %s: %w", path, err)
	}

	if creating {
		if err := os.Chmod(path, newFileMode); err != nil {
			return fmt.Errorf("atomicfile: setting mode on %s: %w", path, err)
		}
	}
	return nil
}

// newFileMode is what a file Forge creates gets. It matches os.WriteFile's
// usual argument rather than the 0600 the temp file is born with; the process
// umask still applies on top.
const newFileMode = 0o644
