package delivery

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"

	qrcode "github.com/yeqown/go-qrcode/v2"
)

const (
	qrQuietZoneModules = 4
	qrModuleScale      = 6
)

type pngQRWriter struct {
	buffer bytes.Buffer
}

func (w *pngQRWriter) Write(matrix qrcode.Matrix) error {
	bitmap := matrix.Bitmap()
	if len(bitmap) == 0 || len(bitmap[0]) == 0 {
		return Failure{Class: ErrorClassPermanent, Code: "qr_render_failed"}
	}
	modules := len(bitmap)
	pixels := (modules + qrQuietZoneModules*2) * qrModuleScale
	img := image.NewGray(image.Rect(0, 0, pixels, pixels))

	white := color.Gray{Y: 255}
	black := color.Gray{Y: 0}
	for y := 0; y < pixels; y++ {
		for x := 0; x < pixels; x++ {
			img.SetGray(x, y, white)
		}
	}

	for y, row := range bitmap {
		for x, dark := range row {
			if !dark {
				continue
			}
			left := (x + qrQuietZoneModules) * qrModuleScale
			top := (y + qrQuietZoneModules) * qrModuleScale
			for py := top; py < top+qrModuleScale; py++ {
				for px := left; px < left+qrModuleScale; px++ {
					img.SetGray(px, py, black)
				}
			}
		}
	}

	if err := png.Encode(&w.buffer, img); err != nil {
		return Failure{Class: ErrorClassPermanent, Code: "qr_render_failed"}
	}
	return nil
}

func (w *pngQRWriter) Close() error { return nil }

func RenderQRCodePNG(payload string) ([]byte, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return nil, Failure{Class: ErrorClassPermanent, Code: "qr_payload_missing"}
	}
	code, err := qrcode.NewWith(payload, qrcode.WithErrorCorrectionLevel(qrcode.ErrorCorrectionMedium))
	if err != nil {
		return nil, Failure{Class: ErrorClassPermanent, Code: "qr_render_failed"}
	}
	writer := &pngQRWriter{}
	if err := code.Save(writer); err != nil {
		return nil, Failure{Class: ErrorClassPermanent, Code: "qr_render_failed"}
	}
	return append([]byte(nil), writer.buffer.Bytes()...), nil
}

func qrAttachmentFilename(profileName string) string {
	profileName = strings.ToLower(strings.TrimSpace(profileName))
	var builder strings.Builder
	previousDash := false
	for _, char := range profileName {
		allowed := (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '_' || char == '-'
		if allowed {
			builder.WriteRune(char)
			previousDash = false
			continue
		}
		if builder.Len() > 0 && !previousDash {
			builder.WriteByte('-')
			previousDash = true
		}
	}
	name := strings.Trim(builder.String(), "-")
	if name == "" {
		name = "vpn"
	}
	if len(name) > 80 {
		name = strings.TrimRight(name[:80], "-")
	}
	return "routegate-" + name + "-qr.png"
}
