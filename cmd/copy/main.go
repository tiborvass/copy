package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/moby/buildkit/util/appcontext"
	"github.com/pkg/errors"
	"github.com/tonistiigi/copy/detect"
	"github.com/tonistiigi/copy/user"
	copy "github.com/tonistiigi/fsutil/copy"
)

// cp with Dockerfile ADD/COPY semantics

type opts struct {
	unpack bool
	chown  *copy.ChownOpt
}

type chown struct {
	uid, gid int
}

func main() {
	var opt opts
	var username string
	flag.BoolVar(&opt.unpack, "unpack", false, "")
	flag.StringVar(&username, "chown", "", "")

	flag.Parse()
	args := flag.Args()

	if username != "" {
		uid, gid, err := user.GetUser(appcontext.Context(), "/", username)
		if err != nil {
			panic(err)
		}
		opt.chown = &copy.ChownOpt{Uid: int(uid), Gid: int(gid)}
	}

	if err := runCopy(appcontext.Context(), args, opt); err != nil {
		panic(err)
	}
}

func runCopy(ctx context.Context, args []string, opt opts) error {
	if len(args) < 2 {
		return fmt.Errorf("invalid args %v", args)
	}

	srcs := args[:len(args)-1]
	isdir := false

	for i, src := range srcs {
		fi, err := os.Lstat(src)
		if err == nil && fi.IsDir() {
			isdir = true
			srcs[i] = path.Clean(src) + "/."
		}
	}

	if len(srcs) > 1 {
		isdir = true
	}

	dest := args[len(args)-1]
	origDest := dest

	if !strings.HasSuffix(dest, "/") && !isdir {
		dest = path.Dir(dest)
	}

	if err := os.MkdirAll(dest, 0700); err != nil {
		return err
	}

	// if target is dir extract or copy all
	fi, err := os.Stat(origDest)
	if err == nil {
		if fi.IsDir() && opt.unpack {
			for _, src := range srcs {
				if err := runUnpack(ctx, src, origDest, detect.DetectArchiveType(src), opt); err != nil {
					return err
				}

			}
			return nil
		}
	}

	// create destination directory for single archive source
	if opt.unpack && len(srcs) == 1 {
		typ := detect.DetectArchiveType(srcs[0])
		if typ != detect.Unknown {
			if err := os.MkdirAll(origDest, 0700); err != nil {
				return err
			}
			if err := runUnpack(ctx, srcs[0], origDest, typ, opt); err != nil {
				return err
			}
			return nil
		}
	}

	return runCp(ctx, srcs, origDest, opt)
}

func runCp(ctx context.Context, srcs []string, dest string, opt opts) error {
	for _, src := range srcs {
		if err := copy.Copy(ctx, src, dest, copy.AllowWildcards, func(ci *copy.CopyInfo) {
			ci.Chown = opt.chown
		}); err != nil {
			return errors.Wrapf(err, "failed to copy %s to %s", src, dest)
		}
	}
	return nil
}

func runUnpack(ctx context.Context, src, dest string, t detect.ArchiveType, opt opts) error {
	if t == detect.Unknown {
		return runCp(ctx, []string{src}, dest, opt)
	}

	// f, err := os.Open(src)
	// if err != nil {
	// 	return errors.Wrapf(err, "failed to open %s", src)
	// }
	//
	// if _, err := archive.Extract(ctx, dest, f); err != nil {
	// 	f.Close()
	// 	return err
	// }
	// return nil
	flags := "-xv"
	switch t {
	case detect.Gzip:
		flags += "z"
	case detect.Bzip2:
		flags += "j"
	case detect.Xz:
		flags += "J"
	}
	cmd := exec.CommandContext(ctx, "tar", flags+"f", src, "-C", dest)
	log.Println("exec", cmd.Path, cmd.Args)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}