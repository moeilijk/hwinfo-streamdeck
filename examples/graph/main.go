package main

import (
	"fmt"
	"image/color"
	"io/ioutil"
	"log"
	"math/rand"
	"time"

	"github.com/moeilijk/hwinfo-streamdeck/pkg/graph"
)

const (
	dev = 40
)

func main() {
	g := graph.NewGraph(72, 72, 0., 100.,
		&color.RGBA{255, 255, 255, 255},
		&color.RGBA{0, 0, 0, 255},
		&color.RGBA{255, 255, 255, 255})
	g.SetLabel(0, "CPU °C", 15, &color.RGBA{183, 183, 183, 255})
	g.SetLabel(1, "5%", 40, &color.RGBA{255, 255, 255, 255})

	data := makeFakeData()
	// data := []float64{
	// 	0., 0., 0., 0., 0.,
	// 	10., 10., 10., 10., 10.,
	// 	20., 20., 20., 20., 20.,
	// 	30., 30., 30., 30., 30.,
	// 	40., 40., 40., 40., 40.,
	// 	50., 50., 50., 50., 50.,
	// 	60., 60., 60., 60., 60.,
	// 	70., 70., 70., 70., 70.,
	// 	80., 80., 80., 80., 80.,
	// 	90., 90., 90., 90., 90.,
	// 	100., 100., 100., 100., 100.,
	// }
	for _, v := range data {
		g.Update(v)
	}
	lastv := data[len(data)-1]

	ticker := time.NewTicker(time.Second)
	for {
		select {
		case <-ticker.C:
			s := rand.NewSource(time.Now().UnixNano())
			r := rand.New(s)
			ndev := r.Intn(dev) - (dev / 2)
			v := lastv + float64(ndev)
			if v > 100 {
				v = 100
			} else if v < 0 {
				v = 0
			}
			fmt.Println(v)
			g.Update(v)
			lastv = v
			bts, err := g.EncodePNG()
			if err != nil {
				log.Fatal("failed to encode png")
			}
			err = ioutil.WriteFile("graph.png", bts, 0644)
			if err != nil {
				log.Fatal("failed to write png")
			}
		}
	}
}

func makeFakeData() []float64 {
	s := rand.NewSource(time.Now().UnixNano())
	r := rand.New(s)
	data := make([]float64, 72)
	v := r.Intn(100)
	lastv := v
	data[0] = float64(v)
	for i := 1; i < 72; i++ {
		ndev := r.Intn(dev) - (dev / 2)
		v = lastv + ndev
		data[i] = float64(v)
	}
	return data
}
