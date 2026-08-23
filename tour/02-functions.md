# 2. 関数とカリー化

[← 目次](README.md) | [前へ: 1. 基本](01-basics.md) | [次へ: 3. オブジェクトとプロトタイプ →](03-objects.md)

## 常に1引数(`weave_spec.md` §5)

**Weaveの関数は常に1引数を取ります。** 複数引数に見える呼び出し`f(a, b, c)`は、`f(a)(b)(c)`(1引数関数を3回連続で呼ぶ)の糖衣構文でしかありません。

```weave
add = fn(a) fn(b) { return a + b }

add5 = add(5)       // 部分適用。「bを待つ関数」が返る
result = add5(3)     // 8
result2 = add(5)(3)   // 上と同じ
```

複数引数のように見える定義`fn(a, b) { ... }`も認められていて、これは`fn(a) fn(b) { ... }`の糖衣構文です(定義側でも呼び出し側でも同じ考え方)。引数の個数に上限はなく、3個以上でも同じ規則で連鎖します。

```weave
main = fn(args) {
	clamp = fn(v, lo, hi) {
		if v < lo { return lo }
		if v > hi { return hi }
		return v
	}
	print(clamp(15, 0, 10))          // 10

	clampTo0_10 = clamp(0, 10)        // vだけ待つ関数(先頭からしか部分適用できない)
	print(clampTo0_10(15))            // 10
	return 0
}
```

## 引数無しの糖衣構文

`fn() {...}`(引数無しの定義)・`f()`(引数無しの呼び出し)という書き方もできます。これは「Weaveの関数は常に1引数」というルール自体が崩れているわけではなく、`fn() {...}`は`fn(_) {...}`(読み取り不可の特別な名前`_`を引数名に使った形)、`f()`は`f(nil)`の糖衣構文です。

```weave
always5 = fn() { return 5 }
print(always5())   // 5(always5(nil)と同じ)
```

ただし`obj.method()`(3章で扱うメソッド呼び出し糖衣構文)には、この`f()`→`f(nil)`の書き換えは適用されません。`obj.method()`は元から「`self`のみを渡す」という意味を持つ特別な形だからです。

## クロージャーと参照捕捉(`weave_spec.md` §10)

関数(カリー化された各段も含む)はレキシカルスコープを持ち、定義時点の外側の変数を**参照で**捕捉します(クロージャー)。値のコピーではなく、同じ変数そのものを見るので、外側の変数がクロージャー生成後に変わっても、クロージャーの中からは変わった後の値が見えます。

```weave
main = fn(args) {
	makeCounter = fn(start) {
		count = start
		return fn(step) {
			count = count + step
			return count
		}
	}
	counter = makeCounter(0)
	print(counter(1))   // 1
	print(counter(1))   // 2(前回の呼び出しの状態を覚えている)
	print(counter(5))   // 7
	return 0
}
```

## 自己再帰

`name = fn(...) {...}`という形で関数リテラルを直接名前に代入した場合、その本体から`name`自身を呼び出せます(参照捕捉のおかげで、クロージャー生成時点ではまだ代入されていない名前も、実際に呼び出される時点までに代入が完了していれば解決できます)。

```weave
main = fn(args) {
	fact = fn(n) {
		if n == 0 { return 1 }
		return n * fact(n - 1)
	}
	print(fact(5))   // 120
	return 0
}
```

自己再帰に限られ、2つの関数が互いを呼び合う相互再帰(`isEven`が`isOdd`を呼び、`isOdd`が`isEven`を呼ぶ)には対応していません。

## `break`/`continue`は関数リテラルの境界を越えない

`while`/`for`の中で定義した関数リテラルの内側に`break`/`continue`を書くとコンパイルエラーになります。ループの中で定義したクロージャーが後から(場合によっては全く別の文脈で)呼ばれた時に、定義されたときのループへ`break`する、という状況は一貫した意味を持たないためです。

```weave
while true {
	f = fn(x) { break }   // コンパイルエラー
}
```

## 演習

1. `fn(a) fn(b) fn(c) { ... }`という3引数のカリー化された関数`volume`を書き、直方体の体積(`a * b * c`)を計算してください。`volume(2, 3, 4)`と`volume(2)(3)(4)`の両方で同じ結果になることを確認してください。
2. `makeCounter`を参考に、呼び出すたびに`1`ずつ増える(引数を取らない)カウンタ`fn() { ... }`を作る関数`makeIncrementer`を書いてください。

<details>
<summary>解答例</summary>

```weave
main = fn(args) {
	volume = fn(a) fn(b) fn(c) { return a * b * c }
	print(volume(2, 3, 4))
	print(volume(2)(3)(4))
	return 0
}
```

```weave
main = fn(args) {
	makeIncrementer = fn(_) {
		count = 0
		return fn() {
			count = count + 1
			return count
		}
	}
	next = makeIncrementer(nil)
	print(next())   // 1
	print(next())   // 2
	print(next())   // 3
	return 0
}
```

`makeIncrementer`自体も1引数関数でなければならないため、使わない引数を`_`(読み取り不可の予約名)で受けています——`fn() { ... }`と書けば`fn(_) { ... }`と同じ意味になります。

</details>

[← 目次](README.md) | [前へ: 1. 基本](01-basics.md) | [次へ: 3. オブジェクトとプロトタイプ →](03-objects.md)
