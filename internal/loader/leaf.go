package loader

import "bytes"

// This file is the leaf layer. Load is the integrator that walks files.

func appendBody(out *bytes.Buffer, body []byte) {
	if len(body) == 0 {
		return
	}
	if out.Len() > 0 && out.Bytes()[out.Len()-1] != '\n' {
		out.WriteByte('\n')
	}
	out.Write(body)
}
