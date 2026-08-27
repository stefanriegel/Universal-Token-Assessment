import { useRef, useState } from 'react';
import { Upload, AlertCircle, X, FileJson } from 'lucide-react';
import {
  importSession,
  mergeSnapshots,
  type LoadedSessionFile,
  type MergeConflict,
  type SessionSnapshot,
} from './session-io';

export interface SessionDropZoneProps {
  onMerged: (snapshot: SessionSnapshot, conflicts: MergeConflict[]) => void;
  onCleared: () => void;
}

interface FileError {
  fileName: string;
  message: string;
}

/** Two files are the same file when both the name and the export timestamp match. */
function isDuplicate(loaded: LoadedSessionFile[], candidate: LoadedSessionFile): boolean {
  return loaded.some(
    (f) => f.fileName === candidate.fileName && f.snapshot.exportedAt === candidate.snapshot.exportedAt
  );
}

/** Stable identity for a loaded file — the same (fileName, exportedAt) pair isDuplicate
 * uses, so two same-named files (e.g. two teams' same-day exports) get distinct keys
 * instead of colliding on fileName alone. */
function fileKey(f: LoadedSessionFile): string {
  return `${f.fileName}::${f.snapshot.exportedAt}`;
}

export function SessionDropZone({ onMerged, onCleared }: SessionDropZoneProps) {
  const [loaded, setLoaded] = useState<LoadedSessionFile[]>([]);
  const [errors, setErrors] = useState<FileError[]>([]);
  const [conflicts, setConflicts] = useState<MergeConflict[]>([]);
  const [dragOver, setDragOver] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  // Mirrors `loaded` so overlapping addFiles() calls (two rapid drops, or a
  // picker selection fired while a previous batch is still awaiting
  // importSession) each commit against the latest list instead of a stale
  // closure. A ref is safe here because every read-then-commit below runs
  // with no `await` in between, so it's atomic under JS's single-threaded
  // scheduling.
  const loadedRef = useRef<LoadedSessionFile[]>([]);

  // Merged state is a pure function of the file list, so every add and remove
  // recomputes from scratch rather than mutating an accumulated snapshot.
  const applyList = (next: LoadedSessionFile[]) => {
    loadedRef.current = next;
    setLoaded(next);
    if (next.length === 0) {
      setConflicts([]);
      onCleared();
      return;
    }
    const result = mergeSnapshots(next);
    setConflicts(result.conflicts);
    onMerged(result.snapshot, result.conflicts);
  };

  const addFiles = async (fileList: FileList | null) => {
    if (!fileList || fileList.length === 0) return;
    const batchErrors: FileError[] = [];
    // Own candidates accepted so far in this batch, not yet committed to
    // loadedRef. Folded into the duplicate check below so within-batch dupes
    // are still caught even though nothing commits until the loop is done.
    const ownAdditions: LoadedSessionFile[] = [];
    for (const file of Array.from(fileList)) {
      try {
        const snapshot = await importSession(file);
        const candidate: LoadedSessionFile = { fileName: file.name, snapshot };
        if (isDuplicate([...loadedRef.current, ...ownAdditions], candidate)) {
          batchErrors.push({ fileName: file.name, message: 'This file is already loaded.' });
          continue;
        }
        ownAdditions.push(candidate);
      } catch (err) {
        batchErrors.push({
          fileName: file.name,
          message: err instanceof Error ? err.message : 'Failed to load session file.',
        });
      }
    }
    // One merge per batch instead of one per file. loadedRef.current is
    // re-read here (not captured before the loop) so a concurrent overlapping
    // batch's commit, made while we were awaiting importSession, isn't lost.
    if (ownAdditions.length > 0) {
      applyList([...loadedRef.current, ...ownAdditions]);
    }
    setErrors(batchErrors);
  };

  const removeFile = (key: string) => {
    applyList(loadedRef.current.filter((f) => fileKey(f) !== key));
  };

  return (
    <div className="mt-4">
      <div
        onDragOver={(e) => { e.preventDefault(); setDragOver(true); }}
        onDragLeave={() => setDragOver(false)}
        onDrop={(e) => { e.preventDefault(); setDragOver(false); void addFiles(e.dataTransfer.files); }}
        className={`border-2 border-dashed rounded-xl p-6 text-center transition-colors ${
          dragOver ? 'border-[var(--infoblox-orange)] bg-orange-50/50' : 'border-gray-300 hover:border-gray-400'
        }`}
      >
        <input
          ref={inputRef}
          data-testid="session-file-input"
          type="file"
          accept=".json"
          multiple
          className="hidden"
          onChange={(e) => { void addFiles(e.target.files); e.target.value = ''; }}
        />
        <Upload className="w-5 h-5 mx-auto mb-2 text-[var(--muted-foreground)]" />
        <p className="text-[13px]" style={{ fontWeight: 600 }}>Load session files</p>
        <p className="text-[11px] text-[var(--muted-foreground)] mt-0.5 mb-3">
          Drop one or more .json session files to combine them into a single assessment.
        </p>
        <button
          type="button"
          onClick={() => inputRef.current?.click()}
          className="px-4 py-2 text-[13px] rounded-xl border border-[var(--border)] bg-white hover:bg-gray-50 transition-colors"
          style={{ fontWeight: 500 }}
        >
          Choose files
        </button>
      </div>

      {loaded.length > 0 && (
        <ul className="mt-3 space-y-2">
          {loaded.map((f) => {
            // Only disambiguate the label with a timestamp when the plain name
            // is genuinely ambiguous — two loaded files sharing a fileName
            // (e.g. two teams' same-day `ddi-session-YYYY-MM-DD.json` exports).
            const sameNameCount = loaded.filter((o) => o.fileName === f.fileName).length;
            const removeLabel = sameNameCount > 1
              ? `Remove ${f.fileName} (${new Date(f.snapshot.exportedAt).toLocaleString()})`
              : `Remove ${f.fileName}`;
            return (
              <li key={fileKey(f)} className="flex items-center gap-2 p-2.5 rounded-lg border border-[var(--border)] bg-white">
                <FileJson className="w-4 h-4 text-[var(--muted-foreground)] shrink-0" />
                <span className="text-[13px]" style={{ fontWeight: 500 }}>{f.fileName}</span>
                <span className="text-[11px] text-[var(--muted-foreground)] truncate">
                  {f.snapshot.selectedProviders.join(', ') || 'no providers'}
                </span>
                <button
                  type="button"
                  aria-label={removeLabel}
                  onClick={() => removeFile(fileKey(f))}
                  className="ml-auto p-1 rounded hover:bg-gray-100 shrink-0"
                >
                  <X className="w-4 h-4 text-[var(--muted-foreground)]" />
                </button>
              </li>
            );
          })}
        </ul>
      )}

      {conflicts.length > 0 && (
        <div className="mt-3 p-3 bg-amber-50 rounded-lg border border-amber-200">
          <p className="text-[12px] text-amber-800 mb-1" style={{ fontWeight: 600 }}>
            Merged with {conflicts.length} conflict{conflicts.length !== 1 ? 's' : ''}
          </p>
          <ul className="list-disc pl-4 space-y-0.5">
            {conflicts.map((c, i) => (
              <li key={`${c.kind}-${c.field}-${i}`} className="text-[12px] text-amber-800">{c.detail}</li>
            ))}
          </ul>
        </div>
      )}

      {errors.map((e) => (
        <div key={e.fileName} className="mt-2 flex items-start gap-2 p-3 bg-red-50 rounded-lg border border-red-200">
          <AlertCircle className="w-4 h-4 text-red-500 mt-0.5 shrink-0" />
          <p className="text-[13px] text-red-700">{e.fileName}: {e.message}</p>
        </div>
      ))}
    </div>
  );
}
