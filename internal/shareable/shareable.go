// Copyright 2018 The Ebiten Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package shareable

import (
	"fmt"
	"image/color"

	"github.com/hajimehoshi/ebiten/internal/affine"
	"github.com/hajimehoshi/ebiten/internal/graphics"
	"github.com/hajimehoshi/ebiten/internal/opengl"
	"github.com/hajimehoshi/ebiten/internal/packing"
	"github.com/hajimehoshi/ebiten/internal/restorable"
	"github.com/hajimehoshi/ebiten/internal/sync"
)

type backend struct {
	restorable *restorable.Image
	page       *packing.Page
}

var (
	// theBackends is a set of actually shared images.
	theBackends = []*backend{}
)

type ImagePart struct {
	backend *backend

	// If node is nil, the image is not shared.
	node *packing.Node
}

func (s *ImagePart) ensureNotShared() {
	if s.node == nil {
		return
	}

	x, y, w, h := s.region()
	newImg := restorable.NewImage(w, h, false)
	newImg.DrawImage(s.backend.restorable, x, y, w, h, nil, nil, opengl.CompositeModeCopy, graphics.FilterNearest)

	s.Dispose()
	s.backend = &backend{
		restorable: newImg,
	}
}

func (s *ImagePart) region() (x, y, width, height int) {
	if s.node == nil {
		w, h := s.backend.restorable.Size()
		return 0, 0, w, h
	}
	return s.node.Region()
}

func (s *ImagePart) Size() (width, height int) {
	_, _, w, h := s.region()
	return w, h
}

func (s *ImagePart) DrawImage(img *ImagePart, sx0, sy0, sx1, sy1 int, geom *affine.GeoM, colorm *affine.ColorM, mode opengl.CompositeMode, filter graphics.Filter) {
	s.ensureNotShared()

	// Compare i and img after ensuring i is not shared, or
	// i and img might share the same texture even though i != img.
	if s.backend.restorable == img.backend.restorable {
		panic("shareable: Image.DrawImage: img must be different from the receiver")
	}

	dx, dy, _, _ := img.region()
	sx0 += dx
	sy0 += dy
	sx1 += dx
	sy1 += dy
	s.backend.restorable.DrawImage(img.backend.restorable, sx0, sy0, sx1, sy1, geom, colorm, mode, filter)
}

func (s *ImagePart) ReplacePixels(p []byte) {
	x, y, w, h := s.region()
	if l := 4 * w * h; len(p) != l {
		panic(fmt.Sprintf("shareable: len(p) was %d but must be %d", len(p), l))
	}
	s.backend.restorable.ReplacePixels(p, x, y, w, h)
}

func (s *ImagePart) At(x, y int) (color.Color, error) {
	ox, oy, w, h := s.region()
	if x < 0 || y < 0 || x >= w || y >= h {
		return color.RGBA{}, nil
	}
	return s.backend.restorable.At(x+ox, y+oy)
}

func (s *ImagePart) isDisposed() bool {
	return s.backend == nil
}

func (s *ImagePart) Dispose() {
	if s.isDisposed() {
		return
	}

	defer func() {
		s.backend = nil
		s.node = nil
	}()

	if s.node == nil {
		s.backend.restorable.Dispose()
		return
	}

	s.backend.page.Free(s.node)
	if !s.backend.page.IsEmpty() {
		return
	}

	index := -1
	for i, sh := range theBackends {
		if sh == s.backend {
			index = i
			break
		}
	}
	if index == -1 {
		panic("not reached")
	}
	theBackends = append(theBackends[:index], theBackends[index+1:]...)
}

func (s *ImagePart) IsInvalidated() (bool, error) {
	return s.backend.restorable.IsInvalidated()
}

var shareableImageLock sync.Mutex

func NewImagePart(width, height int) *ImagePart {
	const maxSize = 2048

	shareableImageLock.Lock()
	defer shareableImageLock.Unlock()

	if width > maxSize || height > maxSize {
		s := &backend{
			restorable: restorable.NewImage(width, height, false),
		}
		return &ImagePart{
			backend: s,
		}
	}

	for _, s := range theBackends {
		if n := s.page.Alloc(width, height); n != nil {
			return &ImagePart{
				backend: s,
				node:    n,
			}
		}
	}
	s := &backend{
		restorable: restorable.NewImage(maxSize, maxSize, false),
		page:       packing.NewPage(maxSize),
	}
	theBackends = append(theBackends, s)

	n := s.page.Alloc(width, height)
	if n == nil {
		panic("not reached")
	}
	return &ImagePart{
		backend: s,
		node:    n,
	}
}

func NewVolatileImagePart(width, height int) *ImagePart {
	r := restorable.NewImage(width, height, true)
	return &ImagePart{
		backend: &backend{
			restorable: r,
		},
	}
}

func NewScreenFramebufferImagePart(width, height int) *ImagePart {
	r := restorable.NewScreenFramebufferImage(width, height)
	return &ImagePart{
		backend: &backend{
			restorable: r,
		},
	}
}