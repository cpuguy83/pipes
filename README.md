# Pipes

A library to work with OS pipes which takes advantage of OS-level optimizations.

## Why pipes?

The idea behind this library is to enable copying between different file
descriptors without the normal overhead of `read(bytes) && write(bytes)`, which
is 2 system calls and 2 copies between userspace and kernel space.

In the implementation here we use the splice(2) system call to move bytes from
the read side to the write side. This means there is only 1 system call to
perform and no data copied into userspace. In many cases there is no copying
even in kernel space as the pointer to the data is just shifted between the two
fd's.

The important thing to note here is that the only benefit that this library
brings is if you are copying between to another file descriptor (such as, but
not limited to, a regular file or a tcp socket).

### Benchmarks

This compares against using io.Copy directly on the underlying *os.File vs the
optimized ReadFrom implementation here.

A note about these benchmarks, as it turns out it is pretty difficult to test
throughput accurately. For instance the tests currently just dumbly drain data
out of the pipe with `io.Copy(ioutil.Discard, pipe)` while the benchmark is in
progress so we can test how write speed. This can in and of itself be a
bottleneck. However, the benchmarks do let us compare throughput across
different implementations with the same bottlenecks.

```
name                           old time/op    new time/op    delta
ReadFrom/regular_file/16K-4      32.9µs ± 0%    16.9µs ± 0%   ~     (p=1.000 n=1+1)
ReadFrom/regular_file/32K-4      43.4µs ± 0%    24.4µs ± 0%   ~     (p=1.000 n=1+1)
ReadFrom/regular_file/64K-4      65.5µs ± 0%    39.1µs ± 0%   ~     (p=1.000 n=1+1)
ReadFrom/regular_file/128K-4      103µs ± 0%      62µs ± 0%   ~     (p=1.000 n=1+1)
ReadFrom/regular_file/256K-4      172µs ± 0%      99µs ± 0%   ~     (p=1.000 n=1+1)
ReadFrom/regular_file/512K-4      300µs ± 0%     178µs ± 0%   ~     (p=1.000 n=1+1)
ReadFrom/regular_file/1MB-4       552µs ± 0%     323µs ± 0%   ~     (p=1.000 n=1+1)
ReadFrom/regular_file/10MB-4     5.26ms ± 0%    3.63ms ± 0%   ~     (p=1.000 n=1+1)
ReadFrom/regular_file/100MB-4    54.7ms ± 0%    35.9ms ± 0%   ~     (p=1.000 n=1+1)
ReadFrom/regular_file/1GB-4       552ms ± 0%     383ms ± 0%   ~     (p=1.000 n=1+1)

name                           old speed      new speed      delta
ReadFrom/regular_file/16K-4     497MB/s ± 0%   967MB/s ± 0%   ~     (p=1.000 n=1+1)
ReadFrom/regular_file/32K-4     755MB/s ± 0%  1345MB/s ± 0%   ~     (p=1.000 n=1+1)
ReadFrom/regular_file/64K-4    1.00GB/s ± 0%  1.68GB/s ± 0%   ~     (p=1.000 n=1+1)
ReadFrom/regular_file/128K-4   1.28GB/s ± 0%  2.12GB/s ± 0%   ~     (p=1.000 n=1+1)
ReadFrom/regular_file/256K-4   1.52GB/s ± 0%  2.64GB/s ± 0%   ~     (p=1.000 n=1+1)
ReadFrom/regular_file/512K-4   1.75GB/s ± 0%  2.94GB/s ± 0%   ~     (p=1.000 n=1+1)
ReadFrom/regular_file/1MB-4    1.90GB/s ± 0%  3.25GB/s ± 0%   ~     (p=1.000 n=1+1)
ReadFrom/regular_file/10MB-4   1.99GB/s ± 0%  2.89GB/s ± 0%   ~     (p=1.000 n=1+1)
ReadFrom/regular_file/100MB-4  1.92GB/s ± 0%  2.92GB/s ± 0%   ~     (p=1.000 n=1+1)
ReadFrom/regular_file/1GB-4    1.94GB/s ± 0%  2.80GB/s ± 0%   ~     (p=1.000 n=1+1)
```
