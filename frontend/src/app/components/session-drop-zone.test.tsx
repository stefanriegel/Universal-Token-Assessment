import { useState } from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { SessionDropZone } from './session-drop-zone';
import { SESSION_FORMAT_VERSION, type SessionSnapshot } from './session-io';
import { EstimatorDefaults } from './estimator-calc';
import type { ProviderType } from './mock-data';
import wizardSrc from './wizard.tsx?raw';

function snapshotFile(
  name: string,
  providers: ProviderType[],
  exportedAt = '2026-01-01T00:00:00.000Z'
): File {
  const snapshot: SessionSnapshot = {
    version: SESSION_FORMAT_VERSION,
    exportedAt,
    toolVersion: '1.0.0',
    selectedProviders: providers,
    findings: [],
    countOverrides: {},
    niosMigrationMap: {},
    adMigrationMap: {},
    niosServerMetrics: [],
    adServerMetrics: [],
    estimatorAnswers: { ...EstimatorDefaults },
    growthBufferPct: 0.2,
    serverGrowthBufferPct: 0.2,
    reportingDestEnabled: {},
    reportingDestEvents: {},
    serverMetricOverrides: {},
  };
  return new File([JSON.stringify(snapshot)], name, { type: 'application/json' });
}

function drop(files: File[]) {
  const input = screen.getByTestId('session-file-input') as HTMLInputElement;
  fireEvent.change(input, { target: { files } });
}

describe('SessionDropZone', () => {
  it('merges several files and lists each one', async () => {
    const onMerged = vi.fn();
    render(<SessionDropZone onMerged={onMerged} onCleared={() => {}} />);
    drop([snapshotFile('aws.json', ['aws']), snapshotFile('azure.json', ['azure'])]);

    await waitFor(() => expect(onMerged).toHaveBeenCalled());
    const [snapshot] = onMerged.mock.calls[onMerged.mock.calls.length - 1];
    expect(snapshot.selectedProviders).toEqual(['aws', 'azure']);
    expect(screen.getByText('aws.json')).toBeTruthy();
    expect(screen.getByText('azure.json')).toBeTruthy();
  });

  it('removing a file recomputes the merge', async () => {
    const onMerged = vi.fn();
    render(<SessionDropZone onMerged={onMerged} onCleared={() => {}} />);
    drop([snapshotFile('aws.json', ['aws']), snapshotFile('azure.json', ['azure'])]);
    await waitFor(() => expect(onMerged).toHaveBeenCalled());

    fireEvent.click(screen.getByLabelText('Remove azure.json'));
    await waitFor(() => {
      const [snapshot] = onMerged.mock.calls[onMerged.mock.calls.length - 1];
      expect(snapshot.selectedProviders).toEqual(['aws']);
    });
  });

  it('calls onCleared when the last file is removed', async () => {
    const onCleared = vi.fn();
    render(<SessionDropZone onMerged={() => {}} onCleared={onCleared} />);
    drop([snapshotFile('aws.json', ['aws'])]);
    await waitFor(() => expect(screen.getByText('aws.json')).toBeTruthy());

    fireEvent.click(screen.getByLabelText('Remove aws.json'));
    await waitFor(() => expect(onCleared).toHaveBeenCalled());
  });

  it('one bad file does not block the rest of the batch', async () => {
    const onMerged = vi.fn();
    render(<SessionDropZone onMerged={onMerged} onCleared={() => {}} />);
    const bad = new File(['not json at all'], 'broken.json', { type: 'application/json' });
    drop([snapshotFile('aws.json', ['aws']), bad]);

    await waitFor(() => expect(onMerged).toHaveBeenCalled());
    const [snapshot] = onMerged.mock.calls[onMerged.mock.calls.length - 1];
    expect(snapshot.selectedProviders).toEqual(['aws']);
    expect(screen.getByText(/broken\.json/)).toBeTruthy();
    expect(screen.getByText(/not valid JSON/i)).toBeTruthy();
  });

  it('leaves state untouched when every file is invalid', async () => {
    const onMerged = vi.fn();
    const onCleared = vi.fn();
    render(<SessionDropZone onMerged={onMerged} onCleared={onCleared} />);
    drop([
      new File(['nope'], 'bad-a.json', { type: 'application/json' }),
      new File(['{"version":99}'], 'bad-b.json', { type: 'application/json' }),
    ]);

    await waitFor(() => expect(screen.getByText(/bad-a\.json/)).toBeTruthy());
    expect(screen.getByText(/bad-b\.json/)).toBeTruthy();
    expect(onMerged).not.toHaveBeenCalled();
    expect(onCleared).not.toHaveBeenCalled();
  });

  it('refuses the same file twice', async () => {
    render(<SessionDropZone onMerged={() => {}} onCleared={() => {}} />);
    drop([snapshotFile('aws.json', ['aws'])]);
    await waitFor(() => expect(screen.getByText('aws.json')).toBeTruthy());

    drop([snapshotFile('aws.json', ['aws'])]);
    await waitFor(() => expect(screen.getByText(/already loaded/i)).toBeTruthy());
    expect(screen.getAllByText('aws.json')).toHaveLength(1);
  });

  it('two same-named files with different export times both load, and removing one keeps the other', async () => {
    const onMerged = vi.fn();
    render(<SessionDropZone onMerged={onMerged} onCleared={() => {}} />);
    // Same fileName, different exportedAt -- e.g. two teams both exporting
    // ddi-session-<date>.json on the same day. Not a duplicate: isDuplicate
    // keys on (fileName, exportedAt).
    drop([
      snapshotFile('ddi-session-2026-07-27.json', ['aws'], '2026-07-27T09:00:00.000Z'),
      snapshotFile('ddi-session-2026-07-27.json', ['azure'], '2026-07-27T14:00:00.000Z'),
    ]);
    await waitFor(() => expect(onMerged).toHaveBeenCalled());
    expect(screen.getAllByText('ddi-session-2026-07-27.json')).toHaveLength(2);

    // The two remove buttons must have distinct, addressable labels -- a
    // fileName-only identity would remove both entries on one click.
    const removeButtons = screen.getAllByRole('button', { name: /^Remove ddi-session-2026-07-27\.json/ });
    expect(removeButtons).toHaveLength(2);

    fireEvent.click(removeButtons[0]);
    await waitFor(() => {
      expect(screen.getAllByText('ddi-session-2026-07-27.json')).toHaveLength(1);
    });
    const [snapshot] = onMerged.mock.calls[onMerged.mock.calls.length - 1];
    expect(snapshot.selectedProviders).toHaveLength(1);
  });

  it('renders merge conflicts without blocking the merge', async () => {
    const onMerged = vi.fn();
    render(<SessionDropZone onMerged={onMerged} onCleared={() => {}} />);
    drop([snapshotFile('a.json', ['aws']), snapshotFile('b.json', ['aws'])]);

    await waitFor(() => expect(onMerged).toHaveBeenCalled());
    expect(screen.getByText(/aws is provided by both/i)).toBeTruthy();
  });

  it('two overlapping drops both survive without clobbering each other', async () => {
    const onMerged = vi.fn();
    render(<SessionDropZone onMerged={onMerged} onCleared={() => {}} />);
    // Fire both batches back to back without awaiting the first — this is the
    // scenario where a stale-closure commit would silently drop one batch.
    drop([snapshotFile('aws.json', ['aws'])]);
    drop([snapshotFile('azure.json', ['azure'])]);

    await waitFor(() => {
      expect(screen.getByText('aws.json')).toBeTruthy();
      expect(screen.getByText('azure.json')).toBeTruthy();
    });
    const [snapshot] = onMerged.mock.calls[onMerged.mock.calls.length - 1];
    expect(snapshot.selectedProviders).toEqual(expect.arrayContaining(['aws', 'azure']));
    expect(snapshot.selectedProviders).toHaveLength(2);
  });
});

// Mimics the wizard's step gate: SessionDropZone lives on the 'providers'
// step only. This host proves the drop zone itself survives a merge without
// unmounting -- it does NOT exercise wizard.tsx's actual onMerged wiring
// (onMerge here is this test's own vi.fn(), which never navigates). The real
// call site in wizard.tsx is covered separately below via wizardSrc.
function WizardStepGateHost({ onMerge }: { onMerge: (snapshot: SessionSnapshot) => void }) {
  const [currentStep, setCurrentStep] = useState<'providers' | 'results'>('providers');
  return (
    <div>
      <p>current step: {currentStep}</p>
      {currentStep === 'providers' && (
        <SessionDropZone onMerged={(snapshot) => onMerge(snapshot)} onCleared={() => {}} />
      )}
      <button onClick={() => setCurrentStep('results')}>Continue</button>
    </div>
  );
}

describe('wizard wiring', () => {
  it('keeps the drop zone mounted across a merge so the file list and remove controls stay reachable', async () => {
    const onMerge = vi.fn();
    render(<WizardStepGateHost onMerge={onMerge} />);

    drop([snapshotFile('aws.json', ['aws'])]);
    await waitFor(() => expect(onMerge).toHaveBeenCalledTimes(1));

    // Still mounted after the merge: the input, loaded-file row and its
    // remove button are all still queryable. Before the fix, onMerged
    // navigated the wizard off the providers step, unmounting the zone here.
    expect(screen.getByTestId('session-file-input')).toBeTruthy();
    expect(screen.getByText('aws.json')).toBeTruthy();
    expect(screen.getByLabelText('Remove aws.json')).toBeTruthy();

    drop([snapshotFile('azure.json', ['azure'])]);
    await waitFor(() => {
      expect(screen.getByText('aws.json')).toBeTruthy();
      expect(screen.getByText('azure.json')).toBeTruthy();
    });

    const lastSnapshot = onMerge.mock.calls[onMerge.mock.calls.length - 1][0] as SessionSnapshot;
    expect(lastSnapshot.selectedProviders).toEqual(['aws', 'azure']);
  });

  it('wires the real wizard.tsx onMerged prop to applySnapshot, not a navigating handler', () => {
    // Reads the actual wizard.tsx source rather than a stand-in host, so
    // reintroducing a navigating handler (the Critical this guards against)
    // fails this assertion.
    expect(wizardSrc).toMatch(
      /<SessionDropZone[\s\S]{0,200}?onMerged=\{\(snapshot\) => applySnapshot\(snapshot\)\}/
    );
  });
});
