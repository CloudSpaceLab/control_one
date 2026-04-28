import {
  flexRender,
  getCoreRowModel,
  getFilteredRowModel,
  getSortedRowModel,
  useReactTable,
  type ColumnDef,
  type SortingState,
} from '@tanstack/react-table';
import { ArrowDown, ArrowUp, ArrowUpDown } from 'lucide-react';
import { useMemo, useState, type ReactNode } from 'react';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { cn } from '@/lib/utils';
import { EmptyState } from './EmptyState';

export interface DataTableProps<T> {
  columns: ColumnDef<T, unknown>[];
  rows: T[];
  rowKey?: (row: T, index: number) => string | number;
  loading?: boolean;
  empty?: ReactNode;
  onRowClick?: (row: T) => void;
  className?: string;
  compact?: boolean;
  sticky?: boolean;
  initialSorting?: SortingState;
  globalFilter?: string;
}

export function DataTable<T>({
  columns,
  rows,
  rowKey,
  loading,
  empty,
  onRowClick,
  className,
  compact,
  sticky,
  initialSorting = [],
  globalFilter,
}: DataTableProps<T>) {
  const [sorting, setSorting] = useState<SortingState>(initialSorting);

  const table = useReactTable({
    data: rows,
    columns,
    state: { sorting, globalFilter },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getRowId: rowKey ? (row, i) => String(rowKey(row, i)) : undefined,
  });

  const skeletonRows = useMemo(() => Array.from({ length: 6 }, (_, i) => i), []);

  return (
    <div
      className={cn(
        'overflow-hidden rounded-lg border border-border-subtle bg-elevated',
        className,
      )}
    >
      <Table>
        <TableHeader className={sticky ? 'sticky top-0 z-10' : undefined}>
          {table.getHeaderGroups().map((hg) => (
            <TableRow key={hg.id}>
              {hg.headers.map((header) => {
                const sortable = header.column.getCanSort();
                const sorted = header.column.getIsSorted();
                return (
                  <TableHead
                    key={header.id}
                    onClick={sortable ? header.column.getToggleSortingHandler() : undefined}
                    className={cn(sortable && 'cursor-pointer select-none hover:text-foreground')}
                    style={{ width: header.getSize() === 150 ? undefined : header.getSize() }}
                  >
                    <span className="inline-flex items-center gap-1.5">
                      {flexRender(header.column.columnDef.header, header.getContext())}
                      {sortable && (
                        sorted === 'asc' ? <ArrowUp className="h-3 w-3" /> :
                        sorted === 'desc' ? <ArrowDown className="h-3 w-3" /> :
                        <ArrowUpDown className="h-3 w-3 opacity-50" />
                      )}
                    </span>
                  </TableHead>
                );
              })}
            </TableRow>
          ))}
        </TableHeader>
        <TableBody>
          {loading
            ? skeletonRows.map((i) => (
                <TableRow key={`s-${i}`}>
                  {columns.map((_, j) => (
                    <TableCell key={j} className={compact ? 'py-2' : undefined}>
                      <Skeleton className="h-4 w-full max-w-[180px]" />
                    </TableCell>
                  ))}
                </TableRow>
              ))
            : table.getRowModel().rows.length === 0
            ? null
            : table.getRowModel().rows.map((row) => (
                <TableRow
                  key={row.id}
                  onClick={onRowClick ? () => onRowClick(row.original) : undefined}
                  className={cn(onRowClick && 'cursor-pointer')}
                >
                  {row.getVisibleCells().map((cell) => (
                    <TableCell key={cell.id} className={compact ? 'py-2' : undefined}>
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </TableCell>
                  ))}
                </TableRow>
              ))}
        </TableBody>
      </Table>
      {!loading && table.getRowModel().rows.length === 0 && (
        <div className="p-4">
          {empty ?? <EmptyState title="No results" description="No rows match the current filters." />}
        </div>
      )}
    </div>
  );
}
