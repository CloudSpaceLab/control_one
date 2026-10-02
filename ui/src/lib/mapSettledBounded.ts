export async function mapSettledBounded<T, R>(
  items: readonly T[],
  worker: (item: T, index: number) => Promise<R>,
  concurrency = 6,
): Promise<PromiseSettledResult<R>[]> {
  if (items.length === 0) return [];

  const workerCount = Math.max(1, Math.min(Math.floor(concurrency) || 1, items.length));
  const results = new Array<PromiseSettledResult<R>>(items.length);
  let cursor = 0;

  const run = async () => {
    for (;;) {
      const index = cursor;
      cursor += 1;
      if (index >= items.length) return;

      try {
        results[index] = {
          status: 'fulfilled',
          value: await worker(items[index], index),
        };
      } catch (reason) {
        results[index] = { status: 'rejected', reason };
      }
    }
  };

  await Promise.all(Array.from({ length: workerCount }, () => run()));
  return results;
}
