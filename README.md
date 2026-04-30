# hbuf_golang



如果你不想自己维护位运算的细节，推荐使用以下库：
github.com/bits-and-blooms/bloom
Go 语言最著名的布隆过滤器库。
它提供了一个 EstimatedFalsePositive 计算，非常专业。
注意：标准版不支持删除，但它有相关的计数实现建议。

github.com/seiflotfy/cuckoofilter
强烈推荐。这是一个 Cuckoo Filter (布谷鸟过滤器)。
优势：原生支持删除，性能比布隆过滤器更好，误判率更低，且实现起来比计数布隆过滤器更优雅。
在很多场景下，它被认为是布隆过滤器的现代替代品。