package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func ensureSeededSelfRepo(workspaceDir string, out io.Writer) error {
	return ensureSeededSelfRepoWithOptions(workspaceDir, out, true)
}

func ensureSelfRepoExists(workspaceDir string) error {
	return ensureSeededSelfRepoWithOptions(workspaceDir, io.Discard, false)
}

func ensureSeededSelfRepoWithOptions(workspaceDir string, out io.Writer, announceExisting bool) error {
	if workspaceDir == "" {
		return fmt.Errorf("workspace directory is not configured")
	}

	selfDir := filepath.Join(workspaceDir, "self")
	info, err := os.Stat(selfDir)
	switch {
	case err == nil:
		if !info.IsDir() {
			return fmt.Errorf("workspace self path %s is not a directory", selfDir)
		}
		if announceExisting {
			fmt.Fprintf(out, "Self repo already exists at %s\n", selfDir)
			fmt.Fprintln(out, "Skipping template copy because bootstrap should never overwrite an existing private self repo.")
		}
		return nil
	case !os.IsNotExist(err):
		return fmt.Errorf("stat %s: %w", selfDir, err)
	}

	templateDir := filepath.Join(workspaceDir, "_self.bootstrap")
	templateInfo, err := os.Stat(templateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("self template directory %s does not exist", templateDir)
		}
		return fmt.Errorf("stat %s: %w", templateDir, err)
	}
	if !templateInfo.IsDir() {
		return fmt.Errorf("self template path %s is not a directory", templateDir)
	}

	fmt.Fprintf(out, "workspace/self does not exist.\n")
	fmt.Fprintf(out, "Seeding it from %s so first-time setup starts with a working private self repo instead of an empty directory.\n", templateDir)
	fmt.Fprintf(out, "Copying template into %s...\n", selfDir)
	if err := copyDir(templateDir, selfDir); err != nil {
		return fmt.Errorf("copy self template: %w", err)
	}
	fmt.Fprintln(out, "Template copy complete.")

	fmt.Fprintln(out, "Re-initializing git after the copy so workspace/self becomes its own agent-specific repo, not a clone of template git metadata.")
	fmt.Fprintf(out, "Initializing a git repo in %s...\n", selfDir)
	gitInit := exec.Command("git", "init")
	gitInit.Dir = selfDir
	gitInit.Stdout = out
	gitInit.Stderr = out
	if err := gitInit.Run(); err != nil {
		return fmt.Errorf("git init in %s: %w", selfDir, err)
	}
	fmt.Fprintln(out, "Git repo initialized.")

	buildScript := filepath.Join(selfDir, "build_clis.sh")
	if _, err := os.Stat(buildScript); err != nil {
		return fmt.Errorf("expected build script at %s: %w", buildScript, err)
	}

	fmt.Fprintf(out, "Building bundled CLIs via %s...\n", buildScript)
	fmt.Fprintln(out, "This happens during bootstrap so the new self repo is immediately usable and toolbox/notesmd are already in place.")
	buildCmd := exec.Command(buildScript)
	buildCmd.Dir = selfDir
	buildCmd.Stdout = out
	buildCmd.Stderr = out
	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("build bundled CLIs in %s: %w", selfDir, err)
	}
	fmt.Fprintln(out, "Bundled CLI build complete.")
	return nil
}

func copyDir(srcDir, dstDir string) error {
	srcInfo, err := os.Stat(srcDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dstDir, srcInfo.Mode().Perm()); err != nil {
		return err
	}

	return filepath.Walk(srcDir, func(srcPath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if srcPath == srcDir {
			return nil
		}

		relPath, err := filepath.Rel(srcDir, srcPath)
		if err != nil {
			return err
		}
		if relPath == ".git" || strings.HasPrefix(relPath, ".git"+string(filepath.Separator)) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		dstPath := filepath.Join(dstDir, relPath)

		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(srcPath)
			if err != nil {
				return err
			}
			return os.Symlink(target, dstPath)
		}

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode().Perm())
		}

		if err := copyFile(srcPath, dstPath, info.Mode().Perm()); err != nil {
			return err
		}
		return nil
	})
}

func copyFile(srcPath, dstPath string, mode os.FileMode) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return err
	}
	if err := dst.Close(); err != nil {
		return err
	}
	if strings.HasSuffix(dstPath, ".sh") {
		if err := os.Chmod(dstPath, mode|0o111); err != nil {
			return err
		}
	}
	return nil
}
