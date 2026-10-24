package research

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
)

func dataArchive(dest string) error {
	f, err := os.Create(filepath.Join(dest, "evidence-data.zip"))
	if err != nil {
		return err
	}
	z := zip.NewWriter(f)
	for _, name := range []string{"worlds.json", "runs.csv", "summary.json", "manifest.json"} {
		src, err := os.Open(filepath.Join(dest, name))
		if err != nil {
			z.Close()
			f.Close()
			return err
		}
		entry, err := z.Create(name)
		if err == nil {
			_, err = io.Copy(entry, src)
		}
		src.Close()
		if err != nil {
			z.Close()
			f.Close()
			return err
		}
	}
	if err := z.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
