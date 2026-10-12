package pdf

import (
	"bytes"
	"crypto/md5"
	"crypto/rc4"
	"encoding/binary"
	"fmt"
	"slices"
)

// passwordPad pads passwords (PDF 2.0, 7.6.4.3.2).
var passwordPad = []byte{
	0x28, 0xbf, 0x4e, 0x5e, 0x4e, 0x75, 0x8a, 0x41, 0x64, 0x00, 0x4e, 0x56, 0xff, 0xfa, 0x01, 0x08,
	0x2e, 0x2e, 0x00, 0xb6, 0xd0, 0x68, 0x3e, 0x80, 0x2f, 0x0c, 0xa9, 0xfe, 0x64, 0x53, 0x69, 0x7a,
}

func rc4Bytes(key, b []byte) []byte {
	c, err := rc4.NewCipher(key)
	if err != nil {
		panic(err)
	}
	out := make([]byte, len(b))
	c.XORKeyStream(out, b)
	return out
}

// rc4Rounds encrypts b with key, then 19 times with key XOR 1..19.
func rc4Rounds(key, b []byte) []byte {
	b = rc4Bytes(key, b)
	for i := 1; i <= 19; i++ {
		k := slices.Clone(key)
		for j := range k {
			k[j] ^= byte(i)
		}
		b = rc4Bytes(k, b)
	}
	return b
}

// encryptedPDF is minimalPDF encrypted with RC4 128 bit (standard security
// handler, revision 3): user and owner password, and the permissions perm
// as /P (-4 is everything).
func encryptedPDF(user, owner string, perm int32) []byte {
	return encryptedContentPDF("1 0 0 rg 100 100 200 200 re f", user, owner, perm)
}

// encryptedContentPDF is encryptedPDF with the content stream content,
// which can use the font /F1 (Helvetica).
func encryptedContentPDF(content, user, owner string, perm int32) []byte {
	padded := func(pw string) []byte { return append([]byte(pw), passwordPad...)[:32] }
	id := []byte("0123456789abcdef")

	// Algorithm 3: /O.
	ok := md5.Sum(padded(owner))
	for range 50 {
		ok = md5.Sum(ok[:])
	}
	o := rc4Rounds(ok[:], padded(user))

	// Algorithm 2: the file key.
	h := md5.New()
	h.Write(padded(user))
	h.Write(o)
	h.Write(binary.LittleEndian.AppendUint32(nil, uint32(perm)))
	h.Write(id)
	key := h.Sum(nil)
	for range 50 {
		s := md5.Sum(key)
		key = s[:]
	}

	// Algorithm 5: /U.
	s := md5.Sum(append(slices.Clone(passwordPad), id...))
	u := append(rc4Rounds(key, s[:]), make([]byte, 16)...)

	// Algorithm 1: the content stream, object 4.
	objKey := md5.Sum(append(slices.Clone(key), 4, 0, 0, 0, 0))
	stream := rc4Bytes(objKey[:], []byte(content))

	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " + helvetica + " >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		fmt.Sprintf("<< /Filter /Standard /V 2 /R 3 /Length 128 /P %d /O <%x> /U <%x> >>", perm, o, u),
	}, fmt.Sprintf("/Encrypt 5 0 R /ID [<%x> <%x>]", id, id))
}

// unsupportedEncryptionPDF is encrypted with a handler no password opens.
func unsupportedEncryptionPDF() []byte {
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Filter /Adobe.PubSec /V 4 /R 4 >>",
	}, "/Encrypt 4 0 R /ID [<00> <00>]")
}

// buildPDF writes objects 1..n with a cross-reference table; trailer is
// added to the trailer dictionary, which has /Root 1 0 R.
func buildPDF(objects []string, trailer string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, obj := range objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R %s >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, trailer, xref)
	return buf.Bytes()
}

// restricted allows everything but printing and copying.
const restricted = int32(-4) &^ int32(PermPrint|PermCopy)
