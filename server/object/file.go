// SPDX-License-Identifier: MIT

package object

import (
	"math"
	"time"

	"github.com/otfabric/go-bacnet"
)

// File_Access_Method property values (BACnetFileAccessMethod). They differ
// from the AtomicReadFile access CHOICE (stream [0], record [1]).
const (
	FileRecordAccess uint32 = 0
	FileStreamAccess uint32 = 1
)

// File errors, with the classes and codes bacnet-stack answers (h_arf.c,
// h_awf.c).
var (
	ErrInconsistentObjectType   = Err(bacnet.ErrorClassServices, bacnet.ErrorCodeInconsistentObjectType)
	ErrInvalidFileAccessMethod  = Err(bacnet.ErrorClassServices, bacnet.ErrorCodeInvalidFileAccessMethod)
	ErrInvalidFileStartPosition = Err(bacnet.ErrorClassServices, bacnet.ErrorCodeInvalidFileStartPosition)
	ErrFileAccessDenied         = Err(bacnet.ErrorClassObject, bacnet.ErrorCodeFileAccessDenied)
	ErrFileFull                 = Err(bacnet.ErrorClassObject, bacnet.ErrorCodeFileFull)
)

// FileAccessor is implemented by File objects for AtomicReadFile and
// AtomicWriteFile (clauses 14.1, 14.2). Start positions and counts are in
// octets for stream access and in records for record access.
type FileAccessor interface {
	StreamAccess() bool
	ReadStream(start int32, count uint32) (data []byte, eof bool, err error)
	WriteStream(start int32, data []byte) (actualStart int32, err error)
	ReadRecords(start int32, count uint32) (records [][]byte, eof bool, err error)
	WriteRecords(start int32, records [][]byte) (actualStart int32, err error)
}

// FileConfig configures a File object.
type FileConfig struct {
	Instance    uint32
	Name        string
	Description string
	FileType    string
	// Record selects record access; the default is stream access.
	Record   bool
	ReadOnly bool
	// MaxSize bounds the file in octets (stream) or records (record);
	// zero means 1 MiB or 65536 records. Above math.MaxInt32 NewFile fails.
	MaxSize int
	Data    []byte
	Records [][]byte
}

// File is an in-memory File object (clause 12.11). A start position of -1
// in AtomicWriteFile appends. A write past the end of the file grows it as
// bacnet-stack's storage does (ports/posix/bacfile-posix.c): a stream file
// is extended with zero octets up to the start position, and records are
// appended after the last one, the actual start being returned. A read past
// the end is refused with invalid-file-start-position, the rule bacnet-stack
// applies to reads (h_arf.c), and AtomicReadFile from the end returns no
// data with End_Of_File set.
type File struct {
	*Base
	cfg      FileConfig
	data     []byte
	records  [][]byte
	modified time.Time
	archive  bool
	now      func() time.Time
}

// NewFile returns a File object.
func NewFile(cfg FileConfig) (*File, error) {
	if cfg.Name == "" {
		return nil, errorf("object name required")
	}
	if cfg.Instance >= bacnet.MaxObjectInstance {
		return nil, errorf("instance %d out of range", cfg.Instance)
	}
	// File start positions and record counts are signed 32-bit in
	// AtomicReadFile and AtomicWriteFile (clause 14).
	if cfg.MaxSize > math.MaxInt32 {
		return nil, errorf("maximum size %d beyond the 32-bit file positions", cfg.MaxSize)
	}
	if cfg.MaxSize <= 0 {
		cfg.MaxSize = 1 << 20
		if cfg.Record {
			cfg.MaxSize = 1 << 16
		}
	}
	f := &File{Base: NewBase(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: cfg.Instance}, cfg.Name), cfg: cfg, now: time.Now}
	f.data = append([]byte(nil), cfg.Data...)
	for _, r := range cfg.Records {
		f.records = append(f.records, append([]byte(nil), r...))
	}
	if (cfg.Record && len(f.records) > cfg.MaxSize) || (!cfg.Record && len(f.data) > cfg.MaxSize) {
		return nil, errorf("initial content exceeds the maximum size")
	}
	f.modified = f.now()
	if cfg.Description != "" {
		f.Define(bacnet.PropertyDescription, &Property{Value: charString(cfg.Description), Writable: true, Kinds: []bacnet.ValueKind{bacnet.ValueCharacterString}})
	}
	f.Define(bacnet.PropertyFileType, &Property{Required: true, Value: charString(cfg.FileType)})
	f.Define(bacnet.PropertyFileSize, &Property{Required: true, Get: func() bacnet.ApplicationValue {
		if f.cfg.Record {
			n := 0
			for _, r := range f.records {
				n += len(r)
			}
			return bacnet.UnsignedValue(uint64(n))
		}
		return bacnet.UnsignedValue(uint64(len(f.data)))
	}})
	f.Define(bacnet.PropertyModificationDate, &Property{Required: true, Get: func() bacnet.ApplicationValue {
		return bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			{Value: bacnet.DateValue(dateOf(f.modified))}, {Value: bacnet.TimeValue(timeOf(f.modified))},
		}}
	}})
	f.Define(bacnet.PropertyArchive, &Property{Required: true, Writable: true,
		Get: func() bacnet.ApplicationValue { return bacnet.BoolValue(f.archive) },
		Set: func(w Write) ([]bacnet.PropertyIdentifier, error) {
			if w.Property.ArrayIndex != nil {
				return nil, ErrNotAnArray
			}
			if w.Value.Kind != bacnet.ValueBoolean {
				return nil, ErrInvalidDataType
			}
			f.archive = w.Value.Boolean
			return []bacnet.PropertyIdentifier{bacnet.PropertyArchive}, nil
		}})
	f.Define(bacnet.PropertyReadOnly, &Property{Required: true, Value: bacnet.BoolValue(cfg.ReadOnly)})
	method := FileStreamAccess
	if cfg.Record {
		method = FileRecordAccess
		f.Define(bacnet.PropertyRecordCount, &Property{Get: func() bacnet.ApplicationValue { return bacnet.UnsignedValue(uint64(len(f.records))) }})
	}
	f.Define(bacnet.PropertyFileAccessMethod, &Property{Required: true, Value: bacnet.EnumValue(method)})
	return f, nil
}

// StreamAccess implements FileAccessor.
func (f *File) StreamAccess() bool { return !f.cfg.Record }

// Data returns a copy of a stream file's content.
func (f *File) Data() []byte {
	var out []byte
	f.View(func() { out = append([]byte(nil), f.data...) })
	return out
}

// Records returns a copy of a record file's records.
func (f *File) Records() [][]byte {
	var out [][]byte
	f.View(func() {
		for _, r := range f.records {
			out = append(out, append([]byte(nil), r...))
		}
	})
	return out
}

// ReadStream implements FileAccessor.
func (f *File) ReadStream(start int32, count uint32) ([]byte, bool, error) {
	if f.cfg.Record {
		return nil, false, ErrInvalidFileAccessMethod
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	if start < 0 || int(start) > len(f.data) {
		return nil, false, ErrInvalidFileStartPosition
	}
	end := len(f.data)
	if uint64(count) < uint64(end-int(start)) {
		end = int(start) + int(count)
	}
	return append([]byte(nil), f.data[start:end]...), end == len(f.data), nil
}

// ReadRecords implements FileAccessor.
func (f *File) ReadRecords(start int32, count uint32) ([][]byte, bool, error) {
	if !f.cfg.Record {
		return nil, false, ErrInvalidFileAccessMethod
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	if start < 0 || int(start) > len(f.records) {
		return nil, false, ErrInvalidFileStartPosition
	}
	end := len(f.records)
	if uint64(count) < uint64(end-int(start)) {
		end = int(start) + int(count)
	}
	var out [][]byte
	for _, r := range f.records[start:end] {
		out = append(out, append([]byte(nil), r...))
	}
	return out, end == len(f.records), nil
}

// WriteStream implements FileAccessor.
func (f *File) WriteStream(start int32, data []byte) (int32, error) {
	if f.cfg.Record {
		return 0, ErrInvalidFileAccessMethod
	}
	if f.cfg.ReadOnly {
		return 0, ErrFileAccessDenied
	}
	var actual int32
	var err error
	f.Update(func() []bacnet.PropertyIdentifier {
		if start < -1 {
			err = ErrInvalidFileStartPosition
			return nil
		}
		at := int(start)
		if start == -1 {
			at = len(f.data)
		}
		// Past the end: the gap reads as zero octets (fseek and fwrite in
		// bacnet-stack ports/posix/bacfile-posix.c).
		if at+len(data) > f.cfg.MaxSize {
			err = ErrFileFull
			return nil
		}
		if at+len(data) > len(f.data) {
			f.data = append(f.data, make([]byte, at+len(data)-len(f.data))...)
		}
		copy(f.data[at:], data)
		actual = int32(at)
		f.modified = f.now()
		return []bacnet.PropertyIdentifier{bacnet.PropertyFileSize, bacnet.PropertyModificationDate}
	})
	return actual, err
}

// WriteRecords implements FileAccessor: the records replace those from
// the start position on, and the file grows when they run past its end.
func (f *File) WriteRecords(start int32, records [][]byte) (int32, error) {
	if !f.cfg.Record {
		return 0, ErrInvalidFileAccessMethod
	}
	if f.cfg.ReadOnly {
		return 0, ErrFileAccessDenied
	}
	var actual int32
	var err error
	f.Update(func() []bacnet.PropertyIdentifier {
		if start < -1 {
			err = ErrInvalidFileStartPosition
			return nil
		}
		at := int(start)
		// Past the end the records are appended after the last one
		// (bacnet-stack ports/posix/bacfile-posix.c reads to the end of
		// the file and writes there).
		if start == -1 || at > len(f.records) {
			at = len(f.records)
		}
		if at+len(records) > f.cfg.MaxSize {
			err = ErrFileFull
			return nil
		}
		for i, r := range records {
			cp := append([]byte(nil), r...)
			if at+i < len(f.records) {
				f.records[at+i] = cp
			} else {
				f.records = append(f.records, cp)
			}
		}
		actual = int32(at)
		f.modified = f.now()
		return []bacnet.PropertyIdentifier{bacnet.PropertyFileSize, bacnet.PropertyRecordCount, bacnet.PropertyModificationDate}
	})
	return actual, err
}
