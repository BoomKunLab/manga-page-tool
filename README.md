# manga-page-tool

漫画のスキャン画像を整えるコマンドラインツールです。画像の形式変換、余白の切り取り、見開きの結合と分割、連番リネームができます。

Go で実装しており、ビルド済みの `mangatool` は単体で実行できます。

## ソースからビルド

```sh
mise trust
mise install
mise run test
mise run build
./bin/mangatool --help
```

Windows では `bin\mangatool.exe` を実行します。Go 1.27.4 以降がインストール済みなら `go build -trimpath -o bin/ ./cmd/mangatool` でもビルドできます。

## 使い方

| コマンド | 動作 |
| --- | --- |
| `mangatool convert jpg pages` | 品質100の JPEG に変換して `pages_jpeg100` に保存。 `jpeg` も指定できます。 |
| `mangatool convert webp pages` | 可逆 WebP に変換して `pages_webp` に保存。 |
| `mangatool convert png pages` | PNG に変換して `pages_png` に保存。 |
| `mangatool crop pages` | 全ページの内容領域を覆う共通枠で切り取り、 `pages_cropped` に保存。 |
| `mangatool append pages/20.png pages/45.png` | 指定ページの左に自然順で次のページを結合。 `20.png` と `21.png` から `21-20.png` を作成。 |
| `mangatool append-all pages` | 自然順で2枚ずつ結合。奇数の場合は最後の1枚を残します。 |
| `mangatool split pages/21-20.png` | 左右を `21.png` と `20.png` に分割。名前にハイフンがなければ `_left` と `_right` を付けます。 |
| `mangatool split-all pages` | 名前にハイフンを含む画像を一括で分割。 |
| `mangatool rename pages 25 1` | 自然順に `25001.png`、 `25002.png` …とリネーム。開始番号を省略すると0。 |

- 一括処理の対象は指定ディレクトリ直下の通常のファイルです。 `convert` は PNG、JPEG、静止 WebP を読み込み、その他の一括処理は PNG と JPEG を対象にします。拡張子の大文字と小文字はどちらも使えます。単独指定の `split` は静止 WebP にも対応します。
- `convert` と `crop` は入力画像を保持します。JPEG は透明部分を白で合成します。WebP は8ビットで保存するため、16ビット PNG を WebP に変換すると各チャンネルの下位8ビットが失われます。再エンコードでは EXIF や ICC プロファイルなどのメタデータは引き継ぎません。
- `crop` は画像の隅の色を基準に20%の許容差で余白を判定します。白紙などの内容領域がない画像は枠の集計から外し、共通枠で切り取って保存します。ImageMagick 版とは画像によって枠が異なる場合があります。
- 結合した見開きは上揃えになり、高さの差は白で埋めます。分割は横幅の中央で行い、幅が奇数なら右側が1ピクセル広くなります。
- `append` と `split` は一括版を含め、処理できた元画像を削除します。 `rename` は元の名前を変更します。既存の出力は上書きせず、出力に失敗した操作の元画像は復元します。複数の見開きは1組ずつ処理し、失敗した組がある場合は終了コード1を返します。
- ファイルの確定にはハードリンクを使うため、出力先のファイルシステムがハードリンクに対応している必要があります。更新中に強制終了した場合は、対象ディレクトリの `.mangatool-*` 内にある `originals` を確認し、必要なファイルを復元してから空の `.mangatool.lock` を取り除いてください。

## バージョン

`mangatool --version` はリリース番号か開発ビルドのコミットIDを表示します。GitHub に `v0.1.0` のようなタグをプッシュすると、GitHub Actions がテスト後に Linux、macOS、Windows 向けのバイナリと SHA-256 チェックサムを GitHub Releases に作成します。

GitHub に公開したタグは、Go または mise でバージョンを指定してインストールできます。

```sh
go install github.com/BoomKunLab/manga-page-tool/cmd/mangatool@v0.1.0
mise use -g go:github.com/BoomKunLab/manga-page-tool/cmd/mangatool@v0.1.0
```

## ライセンス

MIT
