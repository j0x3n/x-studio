package assets

func SetMaxVideoBytes(n int64) func() {
	old := maxVideoBytes
	maxVideoBytes = n
	return func() { maxVideoBytes = old }
}

func SetFFmpegBins(ffmpeg, ffprobe string) func() {
	oldF, oldP := ffmpegBin, ffprobeBin
	ffmpegBin, ffprobeBin = ffmpeg, ffprobe
	return func() { ffmpegBin, ffprobeBin = oldF, oldP }
}
