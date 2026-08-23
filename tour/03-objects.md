# 3. オブジェクトとプロトタイプ

[← 目次](README.md) | [前へ: 2. 関数とカリー化](02-functions.md) | [次へ: 4. リストと組み込み関数 →](04-lists.md)

## オブジェクトリテラルとプロパティ(`weave_spec.md` §4.1)

オブジェクトは`{ }`で作り、プロパティは`.`でアクセスします。プロパティは実行時に自由に追加・削除できます。

```weave
main = fn(args) {
	point = { x: 1, y: 2 }
	print(point.x)   // 1

	point.z = 3        // 追加
	point.x = 10        // 更新
	remove(point, "y")   // 削除(4章で扱う組み込み関数)
	print(point.y)        // nil(削除済み)
	return 0
}
```

存在しないプロパティを読むと、エラーにはならず`nil`が返ります。

```weave
p = { x: 1 }
print(p.y)   // nil(エラーにはならない)
```

## プロトタイプチェーン(`weave_spec.md` §4.2)

`__proto__`という予約プロパティで、オブジェクトの親(プロトタイプ)を指せます。プロパティ読み取り(`obj.name`)は、`obj`自身が持っていなければ`obj.__proto__`を辿って再帰的に探し、どこにも見つからなければ`nil`を返します。

```weave
main = fn(args) {
	base = {
		greet: fn(self) { print(self.name + " says hi") }
	}

	alice = { __proto__: base, name: "Alice" }
	alice.greet()   // "Alice says hi" — baseのgreetを継承して呼べる
	return 0
}
```

`alice.greet()`が`alice.greet(alice)`のように明示的に`alice`を渡す必要が無いことに注目してください。これは次の節で説明する糖衣構文のおかげです。

`__proto__`自体も普通のプロパティなので、オブジェクト生成後に読んだり書き換えたりできます。

```weave
alice.__proto__ = { greet: fn(self) { print("yo, " + self.name) } }
alice.greet()   // 差し替えた後のプロトタイプが使われる
```

## メソッド呼び出し糖衣構文(`weave_spec.md` §9)——0章の統一原理がここに現れる

`obj.method(a, b)`は、実は`obj.method(obj, a, b)`(プロトタイプチェーンで検索した関数に対し、**第1引数として`obj`自身を自動的に差し込む**)の省略形です。0章で触れた「プロパティ読み取り」と「メソッド呼び出し」が同じ仕組みだという話が、ここで具体的な形になります——`.method(...)`という構文は、`obj.method`という**ただのプロパティ読み取り**の結果(関数)に対して、`obj`自身を先頭に追加してから通常のカリー適用をしているだけです。

```weave
counter = {
	count: 0,
	increment: fn(self, n) { self.count = self.count + n },
	get: fn(self) { return self.count }
}
c1 = { __proto__: counter, count: 0 }
c1.increment(5)
c1.increment(3)
print(c1.get())   // 8
```

この自己注入が起きるのは、`.method(...)`という形で**直接**呼び出した最初の1回だけです。`.method`をいったん変数に代入してから呼ぶ場合は、この糖衣構文を経由しないただの関数呼び出しになるため、`self`は自動的には渡りません。

```weave
greeter = { greet: fn(name) { return "hello, " + name } }
greet = greeter.greet   // ただのプロパティ読み取り(糖衣構文の対象外)
print(greet("weave"))    // selfを自分で渡す必要が無い(greetはselfを取らない関数だから)
```

## 型ヒント(任意)——`shape`/`checkShape`(`weave_spec.md` §4.3)

オブジェクトが期待通りのプロパティを持っているかを、任意のタイミングで検証できます。TypeScriptに対するJavaScript、Pythonの型ヒントと同じ位置づけで、実行速度のためではなく安全性・保守性のための機能です。

```weave
main = fn(args) {
	PointShape = shape({ x: "number", y: "number" })

	origin = { x: 0, y: 0 }
	checkShape(PointShape, origin)   // 一致すれば何も起きない
	print("origin matches PointShape")
	return 0
}
```

`"種類"`に書けるのは`"number"`/`"string"`/`"bool"`/`"object"`/`"function"`の5つです。不一致があれば`checkShape`を呼んだ場所で即座に停止し、どのプロパティで・何を期待して・実際は何だったかが分かるエラーになります。

## 演習

1. `{ __proto__: 親, ... }`を使って、`animal`(`describe: fn(self) { return "a " + self.kind }`を持つ)を継承した`dog`(`kind: "dog"`)を作り、`dog.describe()`を呼んで`"a dog"`と表示してください。
2. `{ width: "number", height: "number" }`という形を検証する`shape`を作り、`{ width: 10, height: 20 }`というオブジェクトに対して`checkShape`を呼んでください。次に、`height`が文字列になっている壊れたオブジェクトを渡すとどんなエラーになるか確かめてください。

<details>
<summary>解答例</summary>

```weave
main = fn(args) {
	animal = { describe: fn(self) { return "a " + self.kind } }
	dog = { __proto__: animal, kind: "dog" }
	print(dog.describe())   // "a dog"
	return 0
}
```

```weave
main = fn(args) {
	RectShape = shape({ width: "number", height: "number" })

	ok = { width: 10, height: 20 }
	checkShape(RectShape, ok)
	print("ok matches RectShape")

	broken = { width: 10, height: "20" }
	checkShape(RectShape, broken)   // 実行時エラー: heightがnumberでない
	return 0
}
```

2問目は`checkShape(RectShape, broken)`の行でプログラムが停止します——`weave: property "height" of shape RectShape: expected ^float64, got string`のような、どのプロパティが・何を期待して・実際は何だったかが分かるメッセージになります。

</details>

[← 目次](README.md) | [前へ: 2. 関数とカリー化](02-functions.md) | [次へ: 4. リストと組み込み関数 →](04-lists.md)
