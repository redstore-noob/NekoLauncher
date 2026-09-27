package bindings

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/assetserver"
	optionsassetserver "github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// TestProbeAssetServerServesPluginFile 复现生产资产管线对插件入口请求的响应。
func TestProbeAssetServerServesPluginFile(t *testing.T) {
	dist := os.DirFS(`E:\Project\NyaLauncher\frontend\dist`)
	handler, err := assetserver.NewAssetHandler(optionsassetserver.Options{
		Assets:  dist,
		Handler: NewAssetFallbackHandler(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/plugins/server-status/index.jsx", nil))
	t.Logf("status=%d content-type=%q body-prefix=%.120q",
		recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
}
