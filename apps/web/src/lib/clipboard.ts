/** Copy the shareable URL for a machine and return true on success. */
export async function copyMachineUrl(machineId: string): Promise<boolean> {
	const url = `${location.origin}/c/${machineId}`;
	try {
		await navigator.clipboard.writeText(url);
		return true;
	} catch {
		return false;
	}
}
