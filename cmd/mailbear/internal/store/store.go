// Package store provides persistence backends for submission records.
package store

import (
	"encoding/json"
	"os"

	"github.com/laputalabs/mailbear/cmd/mailbear/internal/domain"
	"github.com/pkg/errors"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

// Options configures a rotating JSONL audit log.
type Options struct {
	Path       string
	MaxSizeMB  int  // rotate once the active file exceeds this size (MB)
	MaxBackups int  // number of rotated files to retain (0 = keep all)
	MaxAgeDays int  // maximum age of rotated files in days (0 = no age limit)
	Compress   bool // gzip rotated files
}

// File is a domain.Store that appends submission records to a size-rotated file
// as JSON Lines (one JSON object per line).
type File struct {
	logger *lumberjack.Logger
}

var _ domain.Store = (*File)(nil)

// NewFile opens (creating if needed) a rotating audit log at opts.Path.
func NewFile(opts Options) (*File, error) {
	// lumberjack opens the file lazily on the first write, so preflight the path
	// here to fail fast at startup on an unwritable location rather than on the
	// first submission.
	f, err := os.OpenFile(opts.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, errors.Wrapf(err, "couldn't open audit log %q", opts.Path)
	}
	if err := f.Close(); err != nil {
		return nil, errors.Wrapf(err, "couldn't close audit log %q", opts.Path)
	}

	return &File{
		logger: &lumberjack.Logger{
			Filename:   opts.Path,
			MaxSize:    opts.MaxSizeMB,
			MaxBackups: opts.MaxBackups,
			MaxAge:     opts.MaxAgeDays,
			Compress:   opts.Compress,
		},
	}, nil
}

// Save appends one record as a single JSON line. lumberjack serialises writes,
// so concurrent calls each append an intact line.
func (s *File) Save(record domain.SubmissionRecord) error {
	line, err := json.Marshal(record)
	if err != nil {
		return errors.Wrap(err, "couldn't encode submission record")
	}
	line = append(line, '\n')

	if _, err := s.logger.Write(line); err != nil {
		return errors.Wrap(err, "couldn't write to audit log")
	}
	return nil
}

// Close closes the underlying log file.
func (s *File) Close() error {
	return s.logger.Close()
}
