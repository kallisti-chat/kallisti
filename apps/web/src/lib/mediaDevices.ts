// Default is already a separate option. Redacted IDs cannot select a
// device, and browser lists may repeat the same ID within a device kind.
export const deviceChoices = (devices: readonly MediaDeviceInfo[], kind: MediaDeviceKind): MediaDeviceInfo[] => {
  const seen = new Set(["", "default"]);
  return devices.filter((device) => {
    if (device.kind !== kind || seen.has(device.deviceId)) return false;
    seen.add(device.deviceId);
    return true;
  });
};
