package pdf

import "sync"

// MuPDFMutex serialisiert prozessweit jede MuPDF-Nutzung (go-fitz). MuPDF
// bricht bei paralleler Nutzung aus mehreren Goroutinen sporadisch mit
// "aborting process from uncaught error!" den gesamten Prozess ab (exit(1)
// im C-Code, in Go nicht abfangbar). Jeder direkte go-fitz-Aufruf - auch
// in anderen Paketen - muss diesen Mutex halten.
var MuPDFMutex sync.Mutex
