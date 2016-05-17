package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"testing"
	"time"
)

func TestPipeOutColdStart(t *testing.T) {
	po, err := NewPipeOut("pipe.out", "1KB", 2)
	if err != nil {
		t.Error("new PipeOut instance fail:", err.Error())
		return
	}
	if po.Path != "pipe.out" || po.MaxBytes != 1024 || po.Backups != 2 {
		t.Error("unexpected PipeOut instance:", po)
		return
	}
	pr, pw := io.Pipe()
	go po.Pipe(pr)
	part1 := bytes.Repeat([]byte("0"), 1024)
	pw.Write(part1)
	time.Sleep(time.Microsecond * 200)
	part2 := bytes.Repeat([]byte("1"), 1024)
	pw.Write(part2)
	time.Sleep(time.Microsecond * 200)
	t.Log("compare round 1")
	if !compareContent(t, "pipe.out.1", part1) {
		return
	}
	part3 := bytes.Repeat([]byte("2"), 1024)
	pw.Write(part3)
	time.Sleep(time.Microsecond * 200)
	t.Log("compare round 2")
	if !compareContent(t, "pipe.out.1", part2) ||
		!compareContent(t, "pipe.out.2", part1) {
		return
	}
	part4 := bytes.Repeat([]byte("3"), 1024)
	pw.Write(part4)
	time.Sleep(time.Microsecond * 200)
	t.Log("compare round 3")
	if !compareContent(t, "pipe.out.1", part3) ||
		!compareContent(t, "pipe.out.2", part2) {
		return
	}
	if _, err := os.Stat("pipe.out.3"); err == nil {
		t.Error("pipe.out.3 should not exist")
		return
	}
	pw.Close()
	t.Log("compare round 4")
	if !compareContent(t, "pipe.out", part4) {
		return
	}
	os.Remove("pipe.out.2")
	os.Remove("pipe.out.1")
	os.Remove("pipe.out")
}

func TestPipeOutWarnStart(t *testing.T) {
	if err := ioutil.WriteFile("pipe.out", bytes.Repeat([]byte("0"), 512), 0755); err != nil {
		t.Error("write pipe.out fail:", err.Error())
		return
	}
	po, err := NewPipeOut("pipe.out", "1KB", 2)
	if err != nil {
		t.Error("new PipeOut instance fail:", err.Error())
		return
	}
	if po.Path != "pipe.out" || po.MaxBytes != 1024 || po.Backups != 2 {
		t.Error("unexpected PipeOut instance:", po)
		return
	}
	pr, pw := io.Pipe()
	go po.Pipe(pr)
	pw.Write(bytes.Repeat([]byte("1"), 1024))
	time.Sleep(time.Microsecond * 200)
	expected := append(bytes.Repeat([]byte("0"), 512), bytes.Repeat([]byte("1"), 512)...)
	if !compareContent(t, "pipe.out.1", expected) {
		return
	}
	pw.Close()
	if !compareContent(t, "pipe.out", bytes.Repeat([]byte("1"), 512)) {
		return
	}
	os.Remove("pipe.out.1")
	os.Remove("pipe.out")
}

func compareContent(t *testing.T, path string, expected []byte) bool {
	if content, err := ioutil.ReadFile(path); err != nil {
		t.Errorf("read %s fail: %s\n", path, err.Error())
		return false
	} else if !bytes.Equal(content, expected) {
		fmt.Println(content)
		fmt.Println(expected)
		t.Errorf("content of %s is not expected", path)
		return false
	}
	return true
}
