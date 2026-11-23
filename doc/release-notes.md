# Oaiso v0.0.10

## 主な変更

- TUIでワーカーを選んで`E`を押すと、共有通信Proxyの拒否履歴を確認できます。許可候補を選ぶと、変更前後の規則、対象ワーカー、既存接続への影響を確認してから適用できます。[通信許可の確認手順](https://github.com/pacificbelt30/sandbox-oaiso/blob/v0.0.10/doc/guides/live-policy.md)
- 終了コードが0以外のバックグラウンドジョブを、内容を確認して再実行できます。元の出力と再実行後の出力は、同じVMのジョブ一覧からそれぞれ開けます。[ジョブの操作手順](https://github.com/pacificbelt30/sandbox-oaiso/blob/v0.0.10/doc/guides/sandbox-ui.md)
- 狭い端末でも操作メニュー、確認画面、状態表示を読みやすくし、キーボードで確認・取消に移動できるようにしました。
- 認証用と一般通信用のProxyの役割分離、およびトークンや通信許可を取り消したときに既存接続が終了することの回帰テストを強化しました。

## 導入と更新

- 新規導入ではLinux x86_64、systemd、KVM、cgroup v2、project quota付きの専用XFSを準備します。[インストール手順](https://github.com/pacificbelt30/sandbox-oaiso/blob/v0.0.10/doc/installation.md)
- 更新前にworkspace、rootfs、専用DBを保管し、VMを削除してからProxyを停止・削除します。v0.0.9からは`oaiso upgrade`で計画を確認できます。v0.0.8以前には、この版の配布インストーラーを使います。[更新手順](https://github.com/pacificbelt30/sandbox-oaiso/blob/v0.0.10/doc/setup.md#更新する)
- 更新後は`oaiso runtime-check`を確認し、対応版のProxyイメージを用意します。起動に失敗すると自動復旧します。明示的に`setup --rollback --apply`を使った後も、到達済みの公開版を記録する下限は維持されます。

## 検証範囲

分離した実Kataノードで、通信拒否から許可までの操作とジョブの再実行を32列×12行の端末で確認しました。画面テストでは24列×10行までの操作と、さらに小さい端末での拡大案内を確認しました。認証・通信の回帰試験には架空の認証情報と試験用の上流を使用しました。

## 配布物

静的Linux amd64バンドル、`install.sh`、`SHA256SUMS`、`sbom.cdx.json`、`provenance.json`を配布します。[署名の確認手順](https://github.com/pacificbelt30/sandbox-oaiso/blob/v0.0.10/doc/guides/signed-releases.md)
