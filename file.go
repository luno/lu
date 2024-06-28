package lu

import (
	"context"
	"os"
	"strconv"

	"github.com/luno/jettison/errors"
	"github.com/luno/jettison/j"
	"github.com/luno/jettison/log"
)

const fileName = "/tmp/lu.pid"

func createPIDFile() error {
	f, err := os.OpenFile(fileName, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o666)
	if errors.Is(err, os.ErrExist) {
		kv := j.MKV{"my_pid": os.Getpid(), "file": fileName, "open_err": err.Error()}
		contents, readErr := os.ReadFile(fileName)
		if readErr != nil {
			// NoReturnErr: Something up with the file, add the error to the original one
			kv["read_err"] = readErr.Error()
		} else {
			kv["existing_pid"] = string(contents)
		}
		return errors.New("process already running", kv)
	}
	defer f.Close()
	_, err = f.WriteString(strconv.Itoa(os.Getpid()))
	if err != nil {
		return errors.Wrap(err, "creating pid file", j.KV("file", fileName))
	}
	return nil
}

func removePIDFile(ctx context.Context) {
	err := os.Remove(fileName)
	if err != nil {
		// NoReturnErr: We'll terminate after this so just log
		log.Error(ctx, errors.Wrap(err, "remove pid file", j.KV("file", fileName)))
	}
}
