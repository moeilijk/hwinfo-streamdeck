package main

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/moeilijk/hwinfo-streamdeck/pkg/graph"
)

func BenchmarkFoo(b *testing.B) {
	_, filename, _, _ := runtime.Caller(0)
	fmt.Println("Current test filename: " + filename)
	os.Chdir(filepath.Dir(filename))

	g := graph.NewGraph(72, 72, 0., 100.,
		&color.RGBA{255, 255, 255, 255},
		&color.RGBA{0, 0, 0, 255},
		&color.RGBA{255, 255, 255, 255})
	g.SetLabel(0, "CPU °C", 15, &color.RGBA{183, 183, 183, 255})
	g.SetLabel(1, "5%", 40, &color.RGBA{255, 255, 255, 255})

	data := []float64{
		0., 0., 0., 0., 0.,
		10., 10., 10., 10., 10.,
		20., 20., 20., 20., 20.,
		30., 30., 30., 30., 30.,
		40., 40., 40., 40., 40.,
		50., 50., 50., 50., 50.,
		60., 60., 60., 60., 60.,
		70., 70., 70., 70., 70.,
		80., 80., 80., 80., 80.,
		90., 90., 90., 90., 90.,
		100., 100., 100., 100., 100.,
		// 0., 0., 0., 0., 0.,
		// 10., 10., 10., 10., 10.,
		// 20., 20., 20., 20., 20.,
		// 30., 30., 30., 30., 30.,
		// 40., 40., 40., 40., 40.,
		// 50., 50., 50., 50., 50.,
		// 60., 60., 60., 60., 60.,
		// 70., 70., 70., 70., 70.,
		// 80., 80., 80., 80., 80.,
		// 90., 90., 90., 90., 90.,
		// 100., 100., 100., 100., 100.,
	}
	_ = data // FIXME
	for i := 0; i < b.N; i++ {
		// FIXME: updateChart does not exist
		// for _, v := range data {
		// 	g.UpdateChart(v)
		// }
		_, err := g.EncodePNG()
		if err != nil {
			b.Fatal("failed to encode png")
		}
	}
}
