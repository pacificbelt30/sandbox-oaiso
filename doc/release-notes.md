# Oaiso v0.0.9

## 主な変更

- 自動セットアップがUbuntu 24.04と26.04を判定し、配置計画に検出した版を表示します。
- WSLでは計画時に専用XFSとproject quotaを確認し、rootでの適用前にKVMで空のVMを作成して解放します。setupは`tun`を読み込み、次回起動用にも設定します。
- Windows 11上のWSL2に向けて、仮想化、カーネルモジュール、専用XFS、再起動後の確認手順を追加しました。
- リポジトリの移転に合わせ、配布物の取得先と署名検証を`pacificbelt30/sandbox-oaiso`へ更新しました。署名付きの旧版v0.0.4〜v0.0.8は、各タグに対して署名当時の作成元を検証します。

## 導入と更新

- Ubuntu 24.04で初めて配置する場合は、[インストール手順](https://github.com/pacificbelt30/sandbox-oaiso/blob/v0.0.9/doc/installation.md)に従い、Linux x86_64、systemd、KVM、cgroup v2、project quota付きの専用XFSを準備します。WSL2では[専用の準備手順](https://github.com/pacificbelt30/sandbox-oaiso/blob/v0.0.9/doc/guides/wsl.md)も確認してください。
- v0.0.8以前からの更新には、この版の配布インストーラーを取得して使います。旧CLIの`upgrade`は移転前のリポジトリ名を使用します。
- [更新手順](https://github.com/pacificbelt30/sandbox-oaiso/blob/v0.0.9/doc/setup.md#更新する)に従ってworkspace、rootfs、専用DBを保管し、VMを削除してからProxyを停止・削除します。更新後は`oaiso runtime-check`を確認し、Proxyイメージを対応版で用意します。
- 起動失敗時は自動復旧します。明示的な`setup --rollback --apply`の後も、到達済みの公開版を記録する下限は維持します。

## 検証範囲

Ubuntu 24.04.5の分離ノードで新規導入、実Kataの基本動作、TTY、キーリング、更新、自動復旧とロールバックを確認しました。WSL向け事前確認はWSLの識別を模擬し、Linux上の実KVMとXFSで確認しました。実WSLのカーネル、Windows／WSL再起動、VPN経由の通信は未検証です。[検証記録](https://github.com/pacificbelt30/sandbox-oaiso/blob/v0.0.9/doc/testing.md)。

## 配布物

静的Linux amd64バンドル、`install.sh`、`SHA256SUMS`、`sbom.cdx.json`、`provenance.json`を配布します。[署名の確認手順](https://github.com/pacificbelt30/sandbox-oaiso/blob/v0.0.9/doc/guides/signed-releases.md)。
