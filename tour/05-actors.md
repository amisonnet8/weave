# 5. アクターモデル

[← 目次](README.md) | [前へ: 4. リストと組み込み関数](04-lists.md) | [次へ: 6. Go資産連携 →](06-go-interop.md)

## スポーン(`weave_spec.md` §6.1)

`spawn(obj)`は、オブジェクト`obj`のコピーを内部状態として持つ**アクター**を新規に起動し、そのアクター参照を返します。アクターは専用の受信キューを持ち、他のコードとは独立して並行に動きます。

```weave
counter = { count: 0 }
a = spawn(counter)
```

## メッセージ送信 = プロトタイプ検索の遠隔呼び出し(`weave_spec.md` §6.2)——0章の統一原理の再登場

`send(a)`は「アクター`a`宛の送信関数」を返します(カリー化)。これにメッセージ名と引数を渡すと、アクターの受信キューへメッセージが積まれます。

```weave
counter = {
	count: 0,
	increment: fn(self, amount) { self.count = self.count + amount },
	get: fn(self, replyTo) { reply(replyTo, self.count) }
}

a = spawn(counter)
send(a)("increment", 5)
send(a)("increment", 3)
```

アクター側では、キューから取り出したメッセージについて「**アクターの内部オブジェクトから、メッセージ名と同じ名前のプロパティ(関数)をプロトタイプチェーン経由で検索し、`self`+メッセージの引数を渡して呼び出す**」という処理を、メッセージが来るたびに繰り返します。0章で見た統一原理そのもので、**アクターの「メッセージハンドラ」を定義する構文は、3章で見たオブジェクトの「メソッド」を定義する構文と全く同じ**です(`increment`/`get`は3章の`self`を取るメソッドと見た目が同じであることに注目してください)。

`send(a)(名前, 値)`は常にメッセージ名+ちょうど1個の値、という形に固定されています(可変長引数ではありません)。複数の値を渡したい場合は、オブジェクトにまとめて1つの値として渡します(`tell("move", { x: 1, y: 2 })`)。

存在しないメッセージ名を送った場合(プロトタイプチェーンのどこにも見つからない関数)、そのメッセージは黙って無視されます。

## `ask`——返信を待つ(`weave_spec.md` §6.3)

`send`は投げっぱなし(fire-and-forget)です。返信が必要な場合は`ask`を使います。

```weave
result = ask(a)("get")   // "get"メッセージを送り、返信を待って受け取る
```

`ask(a)`は`send(a)`と違い、**メッセージ名だけを取る1段のカリー**です——ユーザーが渡す値の引数はありません。`ask`は内部で使い捨ての一時的な受信チャネルを作り、それを`replyTo`としてメッセージ本体に自動的に付け加えてから送信し、その受信チャネルに値が来るまで呼び出し元をブロックします。ハンドラ側は組み込み関数`reply(replyTo, value)`で応答します(上の`get`ハンドラを参照)。`reply`を呼ばないハンドラに対して`ask`を使うと、呼び出し元は永遠にブロックします。

```weave
main = fn(args) {
	counter = {
		count: 0,
		increment: fn(self, amount) { self.count = self.count + amount },
		get: fn(self, replyTo) { reply(replyTo, self.count) }
	}

	a = spawn(counter)
	send(a)("increment", 5)
	send(a)("increment", 3)
	print(ask(a)("get"))   // 8
	return 0
}
```

## 並行性の粒度(`weave_spec.md` §6.4)

1つのアクターは、自分宛のメッセージを**常に1つずつ順番に**処理します(同じアクター内で2つのメッセージが同時に処理されることはありません)。異なるアクター同士は完全に並行に動きます。この性質により、アクター内部の状態(`self`のプロパティ)への読み書きは、アクター内で見る限り競合状態を考えなくてかまいません。

`send`は「投げっぱなし」ですが、宛先のアクターが別のメッセージを処理中で受け取れない場合は、送信側もその処理が終わるまで一時的にブロックされます(アクターの受信キューにはバッファがありません)。「`send`は常に一瞬で返る」という前提でタイミングに依存したコードは書かないでください。

## メッセージハンドラ内のエラー(`weave_spec.md` §6.5)

**メッセージハンドラの実行中に実行時エラーが起きた場合、そのエラーは「送信したメッセージが失敗する」という形では現れず、Weaveプログラム全体がその場で異常終了します。** 1つのアクターだけがクラッシュして他は動き続ける、という部分的な障害分離はありません——アクターは並行性のための仕組みであり、耐障害性のための仕組みではありません。

## 演習

1. `{ balance: 0 }`をプロトタイプとする「銀行口座」アクターを作り、`deposit`(入金)・`withdraw`(出金)・`getBalance`(`ask`で残高を返信)の3つのメッセージハンドラを実装してください。残高を保持する`balance`プロパティとメッセージハンドラの名前が衝突しないよう気をつけてください。
2. アクターを2つ(`a`と`b`)スポーンし、`a`のハンドラの中から`b`へ`ask`でメッセージを送って結果をそのまま`reply`する、という「委譲」を実装してください。

<details>
<summary>解答例</summary>

```weave
main = fn(args) {
	account = {
		balance: 0,
		deposit: fn(self, amount) { self.balance = self.balance + amount },
		withdraw: fn(self, amount) { self.balance = self.balance - amount },
		getBalance: fn(self, replyTo) { reply(replyTo, self.balance) }
	}

	a = spawn(account)
	send(a)("deposit", 100)
	send(a)("withdraw", 30)
	print(ask(a)("getBalance"))   // 70
	return 0
}
```

2問目:

```weave
main = fn(args) {
	bProto = { double: fn(self, replyTo) { reply(replyTo, 21) } }
	aProto = {
		delegate: fn(self, replyTo) {
			b = spawn(bProto)
			reply(replyTo, ask(b)("double"))
		}
	}

	a = spawn(aProto)
	print(ask(a)("delegate"))   // 21
	return 0
}
```

`spawn`/`send`/`ask`は、アクター自身のハンドラの中から呼び出してもかまいません(§6には呼び出し元を制限する記述はありません)。

</details>

[← 目次](README.md) | [前へ: 4. リストと組み込み関数](04-lists.md) | [次へ: 6. Go資産連携 →](06-go-interop.md)
