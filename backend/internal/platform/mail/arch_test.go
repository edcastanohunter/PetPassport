package mail_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

const mailPkg = "github.com/edcastanohunter/petpassport/backend/internal/platform/mail"

// TestARQ014_OnlyWorkerImportsMail: nadie fuera del worker, el adaptador SMTP
// de Notifications y el propio paquete importa platform/mail; el api y los
// módulos encolan con platform/notify (PP-F2-ARQ-014).
func TestARQ014_OnlyWorkerImportsMail(t *testing.T) {
	root := filepath.Join("..", "..", "..") // backend/
	allowed := []string{
		filepath.Join("internal", "platform", "mail"),
		filepath.Join("internal", "notifications", "infra"),
		filepath.Join("cmd", "worker"),
	}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for _, a := range allowed {
			if strings.HasPrefix(rel, a+string(filepath.Separator)) {
				return nil
			}
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		for _, imp := range f.Imports {
			if strings.Trim(imp.Path.Value, `"`) == mailPkg {
				t.Errorf("%s importa platform/mail: solo el worker envía correo (ARQ-014)", rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
