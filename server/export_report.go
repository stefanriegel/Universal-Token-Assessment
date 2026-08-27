package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/stefanriegel/Universal-Token-Assessment/internal/exporter"
)

// maxExportBodyBytes caps the export payload. A large multi-provider merge runs
// to thousands of finding rows; 32 MB is far above any real report and still
// bounds a hostile request.
const maxExportBodyBytes = 32 << 20

// HandleExportReport handles POST /api/v1/export.
//
// The request body is a complete exporter.ReportInput: the report exactly as the
// frontend rendered it, effective counts and displayed totals included. No scan
// session is consulted, so an imported or merged session file exports the same
// workbook a live scan does.
func HandleExportReport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxExportBodyBytes)

	var in exporter.ReportInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "export payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if in.GeneratedAt.IsZero() {
		in.GeneratedAt = time.Now()
	}

	var buf bytes.Buffer
	if err := exporter.Build(&buf, in); err != nil {
		http.Error(w, "export failed", http.StatusInternalServerError)
		return
	}

	filename := "ddi-token-assessment-" + in.GeneratedAt.Format("2006-01-02") + ".xlsx"
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.Header().Set("Cache-Control", "no-store")
	io.Copy(w, &buf) //nolint:errcheck
}
