# 8. パニックの捕捉 —— `recover(...)`

[← 目次](README.md) | [前へ: 7. モジュール](07-modules.md) | [次へ: 9. まとめ →](09-wrapup.md)

Weaveには`try`/`catch`のような、任意のコードブロックを囲んで例外を捕まえる構文はありません。エラーは実行時パニックとしてしか起こらず(1章の型不一致、5章のメッセージハンドラ内エラーなど)、`try`/`catch`に相当する専用構文・例外オブジェクトは存在しません。

その代わり、Go自身の`defer`+`recover()`のイディオムに直接対応する、狭い範囲の回復手段として組み込み関数`recover(handler)`があります(`weave_spec.md` §17)。

## 基本形

```weave
main = fn(args) {
	recover(fn(msg) {
		print("recovered: " + msg)
	})
	riskyOperation()
	return 0
}
```

`recover(handler)`は、それが呼ばれた**その時点から、今実行中の関数(またはクロージャー)の残りの実行**に対して回復ハンドラを仕掛けます——Goの`defer`がそうであるように、`recover(...)`より**前**に終わったコードは対象外です。`handler`は1引数を取る関数(`fn(message) {...}`)で、対象範囲内で実行時パニックが起きた場合に、パニックのメッセージ(文字列)を引数として呼ばれます。

捕捉できるパニックの種類に制限はありません。Weave自身の実行時エラー(8章の演算子の型不一致など)と、Go資産呼び出し(6章)由来のGoパニックは、`recover(...)`にとって全く同じ「実行時パニック」として扱われ、区別なく捕捉できます。

## 回復後、関数はどう終わるか

Goの`defer`+`recover()`をそのまま踏襲しているため、パニックが実際に捕捉された場合、**回復ハンドラの戻り値は捨てられ、パニックが起きた関数自身は即座に、その関数の「空の」値で終了します。**

- 通常のクロージャーの場合は`nil`——明示的な`return`を一度も実行しないまま終わるボディがそもそも`nil`を返すのと同じ挙動(2章)
- `main`自身の場合は終了コード`0`

**回復した後、パニックが起きた地点の続きから実行が再開する、ということはありません。** `recover(...)`は「その関数を丸ごとリトライする」「途中から通常の処理へ戻る」ための仕組みではなく、あくまで「クラッシュを迎える代わりに、後始末を1回だけ実行してから、この関数を諦めて終える」ための仕組みです。

## パニックから値を持ち帰る: ガード用クロージャーのパターン

`handler`自身の戻り値は捨てられるため、「パニックが起きたらフォールバック値を返す」という結果をそのまま呼び出し元へ返すことはできません。この制約を回避するには、**リスクのある処理を専用のヘルパークロージャーへ包み、`recover(...)`をそのクロージャー自身の先頭で呼びます**——そうすれば、パニックが起きてもクラッシュするのはそのヘルパー自身の呼び出しだけで、呼び出し元の関数(`main`等)は普通に次の行から実行を続けられます。`handler`はクロージャーとして外側の変数を参照で捕捉できる(2章)ので、この捕捉を使って結果を外側の変数へ書き戻します。

```weave
main = fn(args) {
	ok = true
	result = nil

	guarded = fn() {
		recover(fn(msg) {
			ok = false
			result = msg   // 失敗時: handlerがresultへ直接書き込む
		})
		result = 1 + "not a number"   // 数値+文字列は実行時型エラーになる
	}
	guarded()   // guarded()自身の戻り値(常にnil)は使わない

	if ok {
		print("succeeded: " + string(result))
	} else {
		print("recovered: " + result)   // "recovered: ..."
	}
	return 0
}
```

**`guarded()`自身の戻り値を`result = guarded()`のように読み取ってはいけません。** パニックして回復した場合、`guarded()`自身の戻り値は(上記の通り)常に`nil`になり、`handler`が直前に書き込んだ`result`を`nil`で踏み潰してしまいます。成功・失敗どちらの経路でも、`result`へ直接書き込む形に統一するのが安全です。

## メッセージハンドラの中では使えない

5章で見た通り、**メッセージハンドラの実行中に実行時エラーが起きると、そのアクターだけでなくプログラム全体が異常終了します。** `recover(...)`はこの「部分的な障害分離は無い」という原則の抜け道になってしまうため、意図的に禁止されています——オブジェクトリテラルのフィールド値として書かれた関数(=`spawn`されればメッセージハンドラになりうる形)の内部で`recover(...)`を呼ぶことはコンパイルエラーになります。

```weave
proto = {
	boom: fn(self, x) {
		recover(fn(msg) { ... })   // コンパイルエラー
		return x + "not a number"
	}
}
```

この検査は構文的なもの(オブジェクトリテラルのフィールド値かどうかを見るだけ)で、`main`の中や、フィールド値ではない普通のクロージャー・関数の中では問題なく`recover(...)`を使えます。

## 演習

1. `sqrt(-1)`(9章まで出てきた組み込み関数、負数を渡すと実行時エラーになる)をガード用クロージャーのパターンで囲み、失敗時に`"sqrt failed"`と表示するプログラムを書いてください。
2. `strconv.Atoi`(6章の`gofunc`)へ数値に変換できない文字列を渡し、`recover(...)`で捕まえて`"invalid number"`と表示するプログラムを書いてください(`raiseIfError`が起こすエラーも、`recover(...)`にとっては他の実行時パニックと同じです)。

<details>
<summary>解答例</summary>

```weave
main = fn(args) {
	ok = true
	guarded = fn() {
		recover(fn(msg) { ok = false })
		sqrt(-1)
	}
	guarded()
	if ok {
		print("sqrt succeeded (unexpected)")
	} else {
		print("sqrt failed")
	}
	return 0
}
```

```weave
main = fn(args) {
	atoi = gofunc("?strconv.Atoi")
	ok = true
	guarded = fn() {
		recover(fn(msg) { ok = false })
		result = atoi("not-a-number")
		raiseIfError(at(result, 1))
	}
	guarded()
	if ok {
		print("parsed (unexpected)")
	} else {
		print("invalid number")
	}
	return 0
}
```

</details>

[← 目次](README.md) | [前へ: 7. モジュール](07-modules.md) | [次へ: 9. まとめ →](09-wrapup.md)
