# 4. リストと組み込み関数

[← 目次](README.md) | [前へ: 3. オブジェクトとプロトタイプ](03-objects.md) | [次へ: 5. アクターモデル →](05-actors.md)

## `list(...)`(`weave_spec.md` §3)

配列相当の値は、「連番の数値キー(`0`, `1`, `2`, ...)を持つ普通のオブジェクト」として表現します。Weaveには専用の配列型が無く、`[1, 2, 3]`のような角括弧の配列リテラル構文もありません——配列が欲しい場合は組み込み関数`list(...)`を使います。

```weave
nums = list(10, 20, 30)
```

## `[index]`による読み書き(`weave_spec.md` §3.1)

```weave
main = fn(args) {
	nums = list(10, 20, 30)
	print(nums[0])       // 10
	nums[1] = 99
	print(nums[1])        // 99
	return 0
}
```

`list[index]`(角括弧)は組み込み関数`at(list, index)`への構文糖衣で、`list[index] = value`という書き込みもできます(書き込みはこの糖衣構文でのみ可能——`at(...)`自体に対応する書き込み関数はありません)。`index`は数値リテラルに限らず、変数や式も書けます。

```weave
i = 1
print(nums[i])          // nums[1]と同じ
print(nums[i + 1])       // nums[2]と同じ
```

`[i][j]`のように連続して書けるので、多次元的にも使えます。

```weave
matrix = list(list(1, 2), list(3, 4))
print(matrix[0][1])   // 2
matrix[1][0] = 30
print(matrix[1][0])    // 30
```

**書き込みは、その位置が既に存在する場合にのみ成功します。** `list(...)`/`at(...)`には要素の追加・削除・挿入・並べ替えといった配列操作が無く、あくまで「複数の値をひとまとめにして受け渡す」程度の用途を想定しています。位置を増やしたい場合は改めて`list(...)`で新しいlistを作ります。

## その他の組み込み関数(`weave_spec.md` §11)

| 関数 | 説明 |
|---|---|
| `has(obj, name)` | `obj`自身(プロトタイプは辿らない)が指定プロパティを持つか判定する |
| `remove(obj, name)` | オブジェクト自身から指定プロパティを削除する |
| `len(obj)` | オブジェクトが自身に持つプロパティの数、または文字列の文字数 |
| `string(value)` | 値を文字列へ変換する |
| `print(value)` | 値を標準出力へ書き、末尾に改行を付ける |

```weave
main = fn(args) {
	point = { x: 1, y: 2 }
	print(has(point, "x"))   // true
	print(len(point))          // 2
	print(string(42))           // "42"
	return 0
}
```

## 文字列操作・数学関数・`exit`(`weave_spec.md` §11)

`+`による文字列結合と`string(...)`変換だけでは足りない場面向けに、文字列・数値それぞれの組み込み関数もそろっています(全一覧は`weave_spec.md` §11)。

```weave
main = fn(args) {
	s = "Hello, Weave!"
	print(contains(s, "Weave"))        // true
	print(indexOf(s, "Weave"))         // 7(見つからなければ-1。at(...)と違いエラーにはならない)
	print(substring(s, 7, 12))         // "Weave"
	print(upper(s))                      // "HELLO, WEAVE!"
	print(replace(s, "Weave", "World"))  // "Hello, World!"

	parts = split("a,b,c", ",")
	print(join(parts, " | "))   // "a | b | c"

	print(floor(1.7))    // 1
	print(round(2.4))     // 2
	print(sqrt(9))         // 3
	return 0
}
```

`indexOf`/`substring`は`len(...)`と同じく文字(Unicode)単位でインデックスを扱います——UTF-8のバイト単位ではありません。

`exit(code)`は`main`の`return`以外でプログラムを即座に終了させる組み込み関数です。呼び出した場所がどこであっても構わず、その場でプロセス全体が終わります——ただし`return`と違い、パニックの捕捉手段`recover(...)`(`weave_spec.md` §20)による捕捉は一切効きません(`os.Exit`自身の挙動そのままで、Goの`defer`機構を経由しないため)。

## `for k, v in obj`によるオブジェクトの列挙(`weave_spec.md` §7)

```weave
main = fn(args) {
	point = { x: 1, y: 2, z: 3 }
	total = 0
	for k, v in point {
		total = total + v
	}
	print(total)   // 6
	return 0
}
```

`for k, v in obj`は`obj`自身が持つプロパティだけを列挙し(プロトタイプは辿らない)、プロパティ名をアルファベット順(文字列としての辞書順)にソートした順で回します。`list(...)`が持つ連番の数値キーについても、これはそのまま数値としての大小順に一致します——`list(0,...,11)`のような10個を超えるlistでも、`"10"`が`"2"`より先に来てしまうようなことはありません。

`list(...)`を要素ごとに処理したいだけなら、1章で見た`while`+手書きカウンタは不要です。`for k, v in list(...)`が位置(`k`)と値(`v`)の両方を順番に渡してくれます。

```weave
main = fn(args) {
	nums = list(10, 20, 30)
	for k, v in nums {
		print(k + ": " + string(v))
	}
	return 0
}
```

`break`/`continue`は`for`でも使えます(1章の`while`と同じ意味論です)。

## 演習

1. `list("apple", "banana", "cherry")`から、`for k, v in`を使って各要素を`"0: apple"`のような形で表示してください。
2. `{ math: 80, science: 90, english: 70 }`という点数のオブジェクトから、`for k, v in`で合計と平均を求めて表示してください(平均は整数に丸めなくてかまいません)。

<details>
<summary>解答例</summary>

```weave
main = fn(args) {
	fruits = list("apple", "banana", "cherry")
	for k, v in fruits {
		print(k + ": " + v)
	}
	return 0
}
```

```weave
main = fn(args) {
	scores = { math: 80, science: 90, english: 70 }
	total = 0
	count = 0
	for k, v in scores {
		total = total + v
		count = count + 1
	}
	print(total)
	print(total / count)
	return 0
}
```

</details>

[← 目次](README.md) | [前へ: 3. オブジェクトとプロトタイプ](03-objects.md) | [次へ: 5. アクターモデル →](05-actors.md)
