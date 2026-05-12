package runner

import "path/filepath"

const (
	// filenameState is the name of the file in the layout where State is persisted.
	stateFilename = "state.json"
	// sourceDirSuffix is the suffix of the per-kind source directories in the layout.
	sourceDirSuffix = "source-v1alpha1"
	// targetDirSuffix is the suffix of the per-kind target directories in the layout.
	targetDirSuffix = "target-v1alpha2"
	// archiveDirSuffix is the suffix of the per-kind archive directories in the layout.
	archiveDirSuffix = "archive"
	// logsDirSuffix is the suffix of the directory where logs are written in the layout.
	logsDirSuffix = "logs"
)

// Layout describes the well-known paths inside a migration workdir.
// All paths are absolute (joined onto Workdir).
type Layout struct {
	Workdir string
}

// NewLayout returns a Layout rooted at workdir.
func NewLayout(workdir string) Layout { return Layout{Workdir: workdir} }

// StateFile is workdir/state.json.
func (l Layout) StateFile() string { return filepath.Join(l.Workdir, stateFilename) }

// SourceDir is workdir/source-v1alpha1.
func (l Layout) SourceDir() string { return filepath.Join(l.Workdir, sourceDirSuffix) }

// TargetDir is workdir/target-v1alpha2.
func (l Layout) TargetDir() string { return filepath.Join(l.Workdir, targetDirSuffix) }

// ArchiveDir is workdir/target-v1alpha2/archive.
func (l Layout) ArchiveDir() string { return filepath.Join(l.TargetDir(), archiveDirSuffix) }

// LogsDir is workdir/logs.
func (l Layout) LogsDir() string { return filepath.Join(l.Workdir, logsDirSuffix) }

// SourceKindDir returns the per-kind source directory.
func (l Layout) SourceKindDir(kind string) string {
	return filepath.Join(l.SourceDir(), kind)
}

// TargetKindDir returns the per-kind apply target directory.
func (l Layout) TargetKindDir(kind string) string {
	return filepath.Join(l.TargetDir(), kind)
}

// ArchiveKindDir returns the per-kind archive directory.
func (l Layout) ArchiveKindDir(kind string) string {
	return filepath.Join(l.ArchiveDir(), kind)
}

// Init creates the top-level directories of the layout. Sub-directories
// (per-kind) are created lazily during the phases that write into them.
func (l Layout) Init() error {
	for _, p := range []string{
		l.Workdir,
		l.SourceDir(),
		l.TargetDir(),
		l.ArchiveDir(),
		l.LogsDir(),
	} {
		if err := mkdirAll(p); err != nil {
			return err
		}
	}
	return nil
}
