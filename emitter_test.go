package main

import (
	"bytes"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEmitter(t *testing.T) {
	wg := new(sync.WaitGroup)
	quit := make(chan int)

	input := NewTestInput()
	output := NewTestOutput(func(data []byte) {
		wg.Done()
	})

	Plugins.Inputs = []io.Reader{input}
	Plugins.Outputs = []io.Writer{output}

	go Start(quit)

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		input.EmitGET()
	}

	wg.Wait()

	close(quit)
}

func TestEmitterFiltered(t *testing.T) {
	wg := new(sync.WaitGroup)
	quit := make(chan int)

	input := NewTestInput()
	input.skipHeader = true

	output := NewTestOutput(func(data []byte) {
		wg.Done()
	})

	Plugins.Inputs = []io.Reader{input}
	Plugins.Outputs = []io.Writer{output}
	methods := HTTPMethods{[]byte("GET")}
	Settings.modifierConfig = HTTPModifierConfig{methods: methods}

	go Start(quit)

	wg.Add(2)

	id := uuid()
	reqh := payloadHeader(RequestPayload, id, time.Now().UnixNano(), -1)
	reqb := append(reqh, []byte("GET / HTTP/1.1\r\nHost: www.w3.org\r\nUser-Agent: Go 1.1 package http\r\nAccept-Encoding: gzip\r\n\r\n")...)

	resh := payloadHeader(ResponsePayload, id, time.Now().UnixNano()+1, 1)
	respb := append(resh, []byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")...)

	input.EmitBytes(reqb)
	input.EmitBytes(respb)

	id = uuid()
	reqh = payloadHeader(RequestPayload, id, time.Now().UnixNano(), -1)
	reqb = append(reqh, []byte("POST / HTTP/1.1\r\nHost: www.w3.org\r\nUser-Agent: Go 1.1 package http\r\nAccept-Encoding: gzip\r\n\r\n")...)

	resh = payloadHeader(ResponsePayload, id, time.Now().UnixNano()+1, 1)
	respb = append(resh, []byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")...)

	input.EmitBytes(reqb)
	input.EmitBytes(respb)

	wg.Wait()

	close(quit)

	Settings.modifierConfig = HTTPModifierConfig{}
}

func TestEmitterSplitRoundRobin(t *testing.T) {
	wg := new(sync.WaitGroup)
	quit := make(chan int)

	input := NewTestInput()

	var counter1, counter2 int32

	output1 := NewTestOutput(func(data []byte) {
		atomic.AddInt32(&counter1, 1)
		wg.Done()
	})

	output2 := NewTestOutput(func(data []byte) {
		atomic.AddInt32(&counter2, 1)
		wg.Done()
	})

	Plugins.Inputs = []io.Reader{input}
	Plugins.Outputs = []io.Writer{output1, output2}

	Settings.splitOutput = true

	go Start(quit)

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		input.EmitGET()
	}

	wg.Wait()

	close(quit)

	if counter1 == 0 || counter2 == 0 {
		t.Errorf("Round robin should split traffic equally: %d vs %d", counter1, counter2)
	}

	Settings.splitOutput = false
}

func TestEmitterRoundRobin(t *testing.T) {
	wg := new(sync.WaitGroup)
	quit := make(chan int)

	input := NewTestInput()

	var counter1, counter2 int32

	output1 := NewTestOutput(func(data []byte) {
		atomic.AddInt32(&counter1, 1)
		wg.Done()
	})

	output2 := NewTestOutput(func(data []byte) {
		atomic.AddInt32(&counter2, 1)
		wg.Done()
	})

	Plugins.Inputs = []io.Reader{input}
	Plugins.Outputs = []io.Writer{output1, output2}

	Settings.splitOutput = true

	go Start(quit)

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		input.EmitGET()
	}

	wg.Wait()

	close(quit)

	if counter1 == 0 || counter2 == 0 {
		t.Errorf("Round robin should split traffic equally: %d vs %d", counter1, counter2)
	}

	Settings.splitOutput = false
}

func TestEmitterSplitSession(t *testing.T) {
	wg1 := new(sync.WaitGroup)
	wg2 := new(sync.WaitGroup)
	wg1.Add(1000)
	wg2.Add(1000)

	// Base uuids, only 1 letter changed
	uuid1 := []byte("1234567890123456789a0000")
	uuid2 := []byte("1234567890123456789d0000")

	quit := make(chan int)

	input := NewTestInput()
	input.skipHeader = true

	var counter1, counter2 int32

	output1 := NewTestOutput(func(data []byte) {
		atomic.AddInt32(&counter1, 1)
		if !bytes.Equal(uuid1[:20], payloadID(data)[:20]) {
			t.Errorf("All tcp sessions should have same id")
		}

		wg1.Done()
	})

	output2 := NewTestOutput(func(data []byte) {
		atomic.AddInt32(&counter2, 1)
		if !bytes.Equal(uuid2[:20], payloadID(data)[:20]) {
			t.Errorf("All tcp sessions should have same id")
		}

		wg2.Done()
	})

	Plugins.Inputs = []io.Reader{input}
	Plugins.Outputs = []io.Writer{output1, output2}

	Settings.splitOutput = true
	Settings.recognizeTCPSessions = true

	go Start(quit)

	for i := 0; i < 1000; i++ {
		// Keep session but randomize ACK
		copy(uuid1[20:], randByte(4))
		input.EmitBytes([]byte("1 " + string(uuid1) + " 1\n" + "GET / HTTP/1.1\r\n\r\n"))
	}

	for i := 0; i < 1000; i++ {
		// Keep session but randomize ACK
		copy(uuid2[20:], randByte(4))
		input.EmitBytes([]byte("1 " + string(uuid2) + " 1\n" + "GET / HTTP/1.1\r\n\r\n"))
	}

	wg1.Wait()
	wg2.Wait()

	close(quit)

	if counter1 != 1000 || counter2 != 1000 {
		t.Errorf("Round robin should split traffic equally: %d vs %d", counter1, counter2)
	}

	Settings.splitOutput = false
	Settings.recognizeTCPSessions = false
}

func BenchmarkEmitter(b *testing.B) {
	wg := new(sync.WaitGroup)
	quit := make(chan int)

	input := NewTestInput()

	output := NewTestOutput(func(data []byte) {
		wg.Done()
	})

	Plugins.Inputs = []io.Reader{input}
	Plugins.Outputs = []io.Writer{output}

	go Start(quit)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		wg.Add(1)
		input.EmitGET()
	}

	wg.Wait()
	close(quit)
}
