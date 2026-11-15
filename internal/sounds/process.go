package sounds

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"time"
)

const durationTolerance = 50 * time.Millisecond

var (
	ErrUnsupportedFormat = errors.New("unsupported sound format: only ogg, wav and mp3 are accepted")
	ErrInvalidSound      = errors.New("file is not a valid sound")
	ErrTooLong           = errors.New("sound is too long")
)

func detectFormat(r io.ReaderAt) (string, error) {
	head := make([]byte, 12)
	n, err := r.ReadAt(head, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	head = head[:n]

	switch {
	case bytes.HasPrefix(head, []byte("OggS")):
		return "ogg", nil
	case len(head) >= 12 && bytes.Equal(head[0:4], []byte("RIFF")) && bytes.Equal(head[8:12], []byte("WAVE")):
		return "wav", nil
	case bytes.HasPrefix(head, []byte("ID3")):
		return "mp3", nil
	case len(head) >= 2 && head[0] == 0xFF && head[1]&0xE0 == 0xE0:
		return "mp3", nil
	}
	return "", ErrUnsupportedFormat
}

type probeResult struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
	} `json:"streams"`
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
	} `json:"format"`
}

func probe(ctx context.Context, path, format string) (time.Duration, error) {
	args := []string{
		"-v", "error",
		"-protocol_whitelist", "file",
		"-show_entries", "stream=codec_type:format=format_name,duration",
		"-of", "json",
	}
	if format != "" {
		args = append(args, "-f", format)
	}
	args = append(args, "-i", path)

	out, err := exec.CommandContext(ctx, "ffprobe", args...).Output()
	if err != nil {
		return 0, ErrInvalidSound
	}

	var res probeResult
	if err := json.Unmarshal(out, &res); err != nil {
		return 0, ErrInvalidSound
	}

	hasAudio := false
	for _, s := range res.Streams {
		if s.CodecType == "audio" {
			hasAudio = true
			break
		}
	}
	if !hasAudio {
		return 0, ErrInvalidSound
	}

	secs, err := strconv.ParseFloat(res.Format.Duration, 64)
	if err != nil || secs <= 0 {
		return 0, ErrInvalidSound
	}
	return time.Duration(secs * float64(time.Second)), nil
}

func convertToOgg(ctx context.Context, src, format, dst string, maxDuration time.Duration) error {
	limit := maxDuration + time.Second
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-v", "error",
		"-nostdin",
		"-protocol_whitelist", "file",
		"-f", format,
		"-i", src,
		"-map", "0:a:0",
		"-vn", "-sn", "-dn",
		"-map_metadata", "-1",
		"-t", strconv.FormatFloat(limit.Seconds(), 'f', -1, 64),
		"-c:a", "libvorbis",
		"-q:a", "5",
		"-flags:a", "+bitexact",
		"-fflags", "+bitexact",
		"-f", "ogg",
		"-y", dst,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: %s", ErrInvalidSound, bytes.TrimSpace(stderr.Bytes()))
	}
	return nil
}

func Process(ctx context.Context, src, dst string, maxDuration time.Duration) (time.Duration, error) {
	tooLong := fmt.Errorf("%w: maximum is %s", ErrTooLong, maxDuration)

	f, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	format, err := detectFormat(f)
	f.Close()
	if err != nil {
		return 0, err
	}

	inDur, err := probe(ctx, src, format)
	if err != nil {
		return 0, err
	}
	if inDur > maxDuration+durationTolerance {
		return 0, tooLong
	}

	if err := convertToOgg(ctx, src, format, dst, maxDuration); err != nil {
		os.Remove(dst)
		return 0, err
	}

	outDur, err := probe(ctx, dst, "ogg")
	if err != nil {
		os.Remove(dst)
		return 0, err
	}
	if outDur > maxDuration+durationTolerance {
		os.Remove(dst)
		return 0, tooLong
	}

	return outDur, nil
}
