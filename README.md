# manga-page-tool

漫画のスキャン画像を整えるコマンドラインツールです。画像の形式変換、余白の切り取り、見開きの結合と分割、連番リネームができます。

Linux（WSL を含む）と macOS に対応しています。ビルド済みの `mangatool` は単体で実行できます。WSL では Linux 版を使います。

## ソースからビルド

```sh
mise trust
mise install
mise run test
mise run build
./bin/mangatool --help
```

Go 1.27.4 以降がインストール済みなら `go build -trimpath -o bin/ ./cmd/mangatool` でもビルドできます。

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
- `crop` は画像の隅の色を基準に20%の許容差で余白を判定します。スクリーンショットの下端に出る UI 線の影響を避けるため、余白を調べる際は最下段の1ピクセル行を除外します。白紙などの内容領域がない画像は枠の集計から外し、出力は元画像を共通枠で切り取ります。ImageMagick 版とは画像によって枠が異なる場合があります。
- 結合した見開きは上揃えになり、高さの差は白で埋めます。分割は横幅の中央で行い、幅が奇数なら右側が1ピクセル広くなります。
- `append` と `split` は一括版を含め、処理できた元画像を削除します。 `rename` は元の名前を変更します。既存の出力は上書きせず、出力に失敗した操作の元画像は復元します。複数の見開きは1組ずつ処理し、失敗した組がある場合は終了コード1を返します。
- ファイルの確定にはハードリンクを使うため、出力先のファイルシステムがハードリンクに対応している必要があります。更新中に強制終了した場合は、対象ディレクトリの `.mangatool-*` 内にある `originals` を確認し、必要なファイルを復元してから空の `.mangatool.lock` を取り除いてください。

`crop` と `convert` は、工程ごとの対象枚数と処理中のファイルを節目ごとに表示します。

## 端末で選ぶ

引数を省略すると、端末で入力フォルダや形式を選べます。

| 起動方法 | 端末で指定する項目 |
| --- | --- |
| `mangatool crop` | 入力フォルダ |
| `mangatool rename` | 入力フォルダ、接頭辞、開始番号 |
| `mangatool rename pages` | 接頭辞、開始番号 |
| `mangatool convert webp` | 入力フォルダ |
| `mangatool convert ./pages` | 変換形式 |
| `mangatool convert` | 変換形式、入力フォルダ |

フォルダ選択はカレントディレクトリから始まります。名前の一部を入力すると候補が絞られます。上下キーで移動し、Tab でフォルダを開き、フォルダ名の行で Enter を押すと確定します。`.` と `..` の行は Enter の確定対象から外しています。`..` の行で Tab を押すと親フォルダへ移動します。カレントディレクトリを切り取る場合は `mangatool crop .` と指定できます。

対話モードのリネームは、変更前後の名前を確認して Enter を押すと実行されます。Esc または Ctrl+C で取り消せます。スクリプトなどからは、すべての引数を指定して実行できます。

## タブ補完

`mangatool` が PATH にある状態で、使っているシェルに応じて次の設定を読み込みます。

Bash（`~/.bashrc`）

```bash
source <(mangatool completion bash)
```

Zsh（`~/.zshrc` の `compinit` より後）

```zsh
source <(mangatool completion zsh)
```

サブコマンド、変換形式、入力ディレクトリ、結合・分割する画像をタブで補完できます。

## バージョン

`mangatool --version` はリリース番号か開発ビルドのコミットIDを表示します。GitHub に `v0.3.3` のようなタグをプッシュすると、GitHub Actions がテスト後に Linux と macOS 向けのバイナリ（各 amd64・arm64）と SHA-256 チェックサムを GitHub Releases に作成します。

GitHub に公開したタグは、Go または mise でバージョンを指定してインストールできます。

```sh
go install github.com/BoomKunLab/manga-page-tool/cmd/mangatool@v0.3.3
mise use -g go:github.com/BoomKunLab/manga-page-tool/cmd/mangatool@v0.3.3
```

## ライセンス

MIT
