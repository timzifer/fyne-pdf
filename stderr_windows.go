//go:build windows

package pdf

/*
#include <windows.h>
#include <stdint.h>
#include <io.h>
#include <fcntl.h>
#include <stdio.h>

// Bindet das übergebene Pipe-Handle als CRT-fd 2 (stderr). Damit landet
// alles, was C-Code über fprintf(stderr, ...) schreibt, in der Pipe.
//
// _dup2 schließt dabei das ORIGINALE stderr-Handle - dasselbe Handle, das
// auch die Go-Runtime (os.Stderr) und via Vererbung Kindprozesse verwenden.
// Deshalb wird das Original vorher dupliziert und das Duplikat per
// SetStdHandle als neues Prozess-stderr hinterlegt. Rückgabe: das Duplikat
// (für Go-seitige Übernahme), 0 wenn es kein gültiges stderr gab, -1 bei
// Fehler in der Umleitung selbst.
static intptr_t pdfRedirectStderr(intptr_t pipeHandle) {
	HANDLE dup = NULL;
	HANDLE orig = GetStdHandle(STD_ERROR_HANDLE);
	if (orig != NULL && orig != INVALID_HANDLE_VALUE) {
		if (DuplicateHandle(GetCurrentProcess(), orig, GetCurrentProcess(),
			&dup, 0, TRUE, DUPLICATE_SAME_ACCESS)) {
			SetStdHandle(STD_ERROR_HANDLE, dup);
		} else {
			dup = NULL;
		}
	}

	int fd = _open_osfhandle(pipeHandle, _O_WRONLY | _O_BINARY);
	if (fd == -1) {
		return -1;
	}
	if (_dup2(fd, 2) == -1) {
		return -1;
	}
	setvbuf(stderr, NULL, _IONBF, 0);
	return (intptr_t)dup;
}
*/
import "C"

import (
	"bufio"
	"os"
	"sync"

	"github.com/cockroachdb/errors"
)

var (
	stderrCaptureOnce sync.Once
	// Pipe-Enden müssen prozesslebenslang referenziert bleiben: das
	// Write-Handle gehört nach _open_osfhandle der C-Runtime, ein
	// Go-Finalizer auf dem os.File würde es unter ihr wegschließen.
	stderrPipeReader *os.File
	stderrPipeWriter *os.File
)

// CaptureCStderr leitet den stderr der C-Runtime (CRT-fd 2) prozessweit in
// eine Pipe um und ruft handler für jede geschriebene Zeile auf. MuPDF
// (go-fitz) meldet Warnungen und Fehler ausschließlich über
// fprintf(stderr, "warning: ...") bzw. "error: ..." - go-fitz bietet keinen
// Warning-Callback an, daher wird der Stream hier abgefangen.
//
// os.Stderr wird dabei auf ein Duplikat des ursprünglichen stderr-Handles
// umgehängt (die C-Runtime schließt das Original beim Umleiten): Go-seitige
// Ausgaben und per SetStdHandle auch Kindprozesse (CEF-Hilfsprozesse!)
// schreiben weiterhin auf das echte stderr, nicht in die Pipe. DLLs mit
// eigener CRT (z.B. libcef.dll) haben eine eigene fd-Tabelle und sind von
// der Umleitung ohnehin unberührt.
//
// Die Umleitung ist nicht rückgängig zu machen; weitere Aufrufe sind No-ops.
// handler läuft auf einer eigenen Goroutine und darf blockieren, sollte aber
// zügig zurückkehren, damit die Pipe nicht volläuft.
func CaptureCStderr(handler func(line string)) error {
	var err error
	stderrCaptureOnce.Do(func() {
		var r, w *os.File
		r, w, err = os.Pipe()
		if err != nil {
			err = errors.Wrap(err, "could not create pipe for C stderr")
			return
		}
		dup := C.pdfRedirectStderr(C.intptr_t(w.Fd()))
		if dup == -1 {
			_ = r.Close()
			_ = w.Close()
			err = errors.New("could not redirect C stderr to pipe")
			return
		}
		if dup != 0 {
			os.Stderr = os.NewFile(uintptr(dup), "stderr")
		}
		stderrPipeReader = r
		stderrPipeWriter = w
		go func() {
			scanner := bufio.NewScanner(r)
			for scanner.Scan() {
				handler(scanner.Text())
			}
		}()
	})
	return err
}
