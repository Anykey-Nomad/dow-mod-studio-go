package main

import (
	"image"

	engine "dow-mod-studio/internal/3d_engine"
	"github.com/fogleman/fauxgl"
)

type modelTexture = engine.Texture

func newModelTexture(img image.Image) fauxgl.Texture { return engine.NewTexture(img) }
