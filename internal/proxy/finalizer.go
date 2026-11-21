package proxy

import (
	"fmt"
	"io/fs"
)

// osFinalizer is the real Finalizer: a no-replace rename per OS (T-16 F8, TH-12). os.Rename and
// os.Root.Rename both replace an existing target, so the plain call is never used unless the
// caller asked for replace after checking the target.
type osFinalizer struct{}

// NewFinalizer returns the Finalizer of the running OS. It works on a *RootFS only.
func NewFinalizer() Finalizer { return osFinalizer{} }

// Rename implements Finalizer.
func (osFinalizer) Rename(fsys FS, from, to string, replace bool) error {
	r, ok := fsys.(*RootFS)
	if !ok {
		return fmt.Errorf("proxy: the OS finalizer needs a *RootFS, got %T", fsys)
	}
	if !fs.ValidPath(from) || !fs.ValidPath(to) { // no "..", no absolute name, on every OS
		return fmt.Errorf("proxy: finalize names must stay inside the root: %w", fs.ErrInvalid)
	}
	if replace {
		return r.root.Rename(from, to)
	}
	return renameNoReplace(r, from, to)
}
