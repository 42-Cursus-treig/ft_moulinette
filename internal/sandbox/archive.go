package sandbox

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// extractArchive décompresse une archive (.zip, .tar.gz/.tgz ou .rar) dans un
// dossier temporaire et renvoie son chemin.
func extractArchive(archivePath string) (string, error) {
	dir, err := os.MkdirTemp("", "ft-moulinette-*")
	if err != nil {
		return "", err
	}

	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		err = extractZip(archivePath, dir)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		err = extractTarGz(archivePath, dir)
	case strings.HasSuffix(lower, ".rar"):
		err = extractRar(archivePath, dir)
	default:
		err = fmt.Errorf("format d'archive non supporté (zip, tar.gz ou rar attendu)")
	}

	if err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

// safeJoin empêche le "zip slip" : une entrée "../../etc/passwd" ne doit jamais
// pouvoir écrire hors du dossier de destination.
func safeJoin(dest, name string) (string, error) {
	fpath := filepath.Join(dest, name)
	if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
		return "", fmt.Errorf("chemin invalide dans l'archive: %s", name)
	}
	return fpath, nil
}

func extractZip(path, dest string) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		fpath, err := safeJoin(dest, f.Name)
		if err != nil {
			return err
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(fpath, 0755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), 0755); err != nil {
			return err
		}

		out, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			out.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func extractTarGz(path, dest string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		fpath, err := safeJoin(dest, hdr.Name)
		if err != nil {
			return err
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(fpath, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(fpath), 0755); err != nil {
				return err
			}
			out, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			_, err = io.Copy(out, tr)
			out.Close()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// extractRar délègue à unrar.
func extractRar(path, dest string) error {
	if _, err := exec.LookPath("unrar"); err != nil {
		return fmt.Errorf("unrar n'est pas installé sur le serveur (sudo apt install unrar)")
	}

	cmd := exec.Command("unrar", "x", "-o+", "-inul", path, dest+string(os.PathSeparator))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %s", err, out)
	}
	return nil
}
