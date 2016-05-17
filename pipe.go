package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	bufferSize = 10240
	readSleep  = time.Microsecond * 100
)

type PipeOut struct {
	Path     string
	MaxBytes int64
	Backups  int
	file     *os.File
	size     int64
	err      error
}

func NewPipeOut(path string, maxBytes string, backups int) (*PipeOut, error) {
	var size int64
	var err error
	if strings.HasSuffix(maxBytes, "KB") {
		size, err = strconv.ParseInt(maxBytes[:len(maxBytes)-2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid maxbytes: %s", maxBytes)
		}
		size *= 1024
	} else if strings.HasSuffix(maxBytes, "MB") {
		size, err = strconv.ParseInt(maxBytes[:len(maxBytes)-2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid maxbytes: %s", maxBytes)
		}
		size *= 1024 * 1024
	} else if strings.HasSuffix(maxBytes, "GB") {
		size, err = strconv.ParseInt(maxBytes[:len(maxBytes)-2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid maxbytes: %s", maxBytes)
		}
		size *= 1024 * 1024 * 1024
	} else {
		size, err = strconv.ParseInt(maxBytes[:len(maxBytes)-2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid maxbytes: %s", maxBytes)
		}
	}
	po := &PipeOut{
		Path:     path,
		MaxBytes: size,
		Backups:  backups,
	}
	if err := po.open(); err != nil {
		return nil, err
	}
	return po, nil
}

func (po *PipeOut) Pipe(r io.Reader) {
	buf := make([]byte, bufferSize)
	for {
		n, err := r.Read(buf)
		if err == io.EOF {
			po.file.Close()
			break
		} else if err != nil {
			po.logError(err, "[mini-supervisor]read pipe fail: %s\n", err.Error())
			time.Sleep(readSleep)
		} else if n == 0 {
			po.err = nil
			time.Sleep(readSleep)
		} else {
			po.write(buf[:n])
		}
	}
}

func (po *PipeOut) logError(err error, format string, params ...interface{}) {
	if po.err == nil {
		po.err = err
		fmt.Fprintf(os.Stderr, format, params...)
	}
}

func (po *PipeOut) write(buf []byte) {
	if po.file == nil {
		return
	}
	bufLen := int64(len(buf))
	if bufLen+po.size > po.MaxBytes {
		remain := int(po.MaxBytes - po.size)
		if _, err := po.file.Write(buf[:remain]); err != nil {
			po.logError(err, "[mini-supervisor]write file fail: %s\n", err.Error())
			return
		}
		if err := po.rotate(); err != nil {
			po.logError(err, "[mini-supervisor]write file fail: %s\n", err.Error())
			return
		}
		buf = buf[remain:]
		po.size = 0
	}
	n, err := po.file.Write(buf)
	po.size += int64(n)
	if err != nil {
		po.logError(err, "[mini-supervisor]write file fail: %s\n", err.Error())
		return
	}
	if err := po.file.Sync(); err != nil {
		po.logError(err, "[mini-supervisor]sync file fail: %s\n", err.Error())
		return
	}
	po.err = nil
}

func (po *PipeOut) rotate() error {
	if err := po.file.Close(); err != nil {
		return err
	}
	po.file = nil
	if po.Backups == 0 {
		os.Remove(po.Path)
		return po.open()
	}
	last := fmt.Sprintf("%s.%d", po.Path, po.Backups)
	os.Remove(last)
	for i := po.Backups - 1; i > 0; i-- {
		from := fmt.Sprintf("%s.%d", po.Path, i)
		to := fmt.Sprintf("%s.%d", po.Path, i+1)
		os.Rename(from, to)
	}
	os.Rename(po.Path, po.Path+".1")
	return po.open()
}

func (po *PipeOut) open() error {
	info, err := os.Stat(po.Path)
	if err != nil {
		po.size = 0
	} else {
		po.size = info.Size()
	}
	file, err := os.OpenFile(po.Path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0755)
	if err != nil {
		return err
	}
	po.file = file
	return nil
}
