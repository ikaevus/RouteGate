import { t } from '../i18n/i18n';

type EncodedQrCode = {
  modules: boolean[][];
  size: number;
};

type QrVersionSpec = {
  version: number;
  dataCodewords: number;
  errorCorrectionCodewords: number;
  blocks: number;
  alignmentPatterns: number[];
};

type ScannableQrCodeProps = {
  value?: string | null;
  title?: string;
  subtitle?: string;
  showHeader?: boolean;
};

const LOW_ERROR_CORRECTION_FORMAT_BITS = 1;
const MASK_PATTERN = 0;
const QUIET_ZONE_MODULES = 4;

const VERSION_SPECS: QrVersionSpec[] = [
  { version: 1, dataCodewords: 19, errorCorrectionCodewords: 7, blocks: 1, alignmentPatterns: [] },
  { version: 2, dataCodewords: 34, errorCorrectionCodewords: 10, blocks: 1, alignmentPatterns: [6, 18] },
  { version: 3, dataCodewords: 55, errorCorrectionCodewords: 15, blocks: 1, alignmentPatterns: [6, 22] },
  { version: 4, dataCodewords: 80, errorCorrectionCodewords: 20, blocks: 1, alignmentPatterns: [6, 26] },
  { version: 5, dataCodewords: 108, errorCorrectionCodewords: 26, blocks: 1, alignmentPatterns: [6, 30] },
  { version: 6, dataCodewords: 136, errorCorrectionCodewords: 18, blocks: 2, alignmentPatterns: [6, 34] },
  { version: 7, dataCodewords: 156, errorCorrectionCodewords: 20, blocks: 2, alignmentPatterns: [6, 22, 38] },
  { version: 8, dataCodewords: 194, errorCorrectionCodewords: 24, blocks: 2, alignmentPatterns: [6, 24, 42] },
  { version: 9, dataCodewords: 232, errorCorrectionCodewords: 30, blocks: 2, alignmentPatterns: [6, 26, 46] },
];

const GF_EXP = new Array<number>(512).fill(0);
const GF_LOG = new Array<number>(256).fill(0);

let value = 1;
for (let index = 0; index < 255; index += 1) {
  GF_EXP[index] = value;
  GF_LOG[value] = index;
  value <<= 1;

  if ((value & 0x100) !== 0) {
    value ^= 0x11d;
  }
}

for (let index = 255; index < GF_EXP.length; index += 1) {
  GF_EXP[index] = GF_EXP[index - 255];
}

function appendBits(bits: number[], valueToAppend: number, length: number) {
  for (let index = length - 1; index >= 0; index -= 1) {
    bits.push((valueToAppend >>> index) & 1);
  }
}

function getBit(valueToRead: number, index: number): boolean {
  return ((valueToRead >>> index) & 1) !== 0;
}

function multiplyFieldValues(left: number, right: number): number {
  if (left === 0 || right === 0) {
    return 0;
  }

  return GF_EXP[GF_LOG[left] + GF_LOG[right]];
}

function createErrorCorrectionGenerator(degree: number): number[] {
  let coefficients = [1];

  for (let index = 0; index < degree; index += 1) {
    const next = new Array<number>(coefficients.length + 1).fill(0);
    const root = GF_EXP[index];

    coefficients.forEach((coefficient, coefficientIndex) => {
      next[coefficientIndex] ^= coefficient;
      next[coefficientIndex + 1] ^= multiplyFieldValues(coefficient, root);
    });

    coefficients = next;
  }

  return coefficients;
}

function createErrorCorrectionCodewords(data: number[], degree: number): number[] {
  const generator = createErrorCorrectionGenerator(degree);
  const message = [...data, ...new Array<number>(degree).fill(0)];

  data.forEach((_, index) => {
    const factor = message[index];

    if (factor === 0) {
      return;
    }

    generator.forEach((coefficient, coefficientIndex) => {
      message[index + coefficientIndex] ^= multiplyFieldValues(coefficient, factor);
    });
  });

  return message.slice(message.length - degree);
}

function buildDataCodewords(payloadBytes: Uint8Array, spec: QrVersionSpec): number[] {
  const bits: number[] = [];

  appendBits(bits, 0b0100, 4);
  appendBits(bits, payloadBytes.length, 8);

  payloadBytes.forEach((byte) => appendBits(bits, byte, 8));

  const capacityBits = spec.dataCodewords * 8;
  const terminatorLength = Math.min(4, capacityBits - bits.length);
  appendBits(bits, 0, terminatorLength);

  while (bits.length % 8 !== 0) {
    bits.push(0);
  }

  const codewords: number[] = [];
  for (let index = 0; index < bits.length; index += 8) {
    let codeword = 0;

    for (let bitIndex = 0; bitIndex < 8; bitIndex += 1) {
      codeword = (codeword << 1) | bits[index + bitIndex];
    }

    codewords.push(codeword);
  }

  for (let index = 0; codewords.length < spec.dataCodewords; index += 1) {
    codewords.push(index % 2 === 0 ? 0xec : 0x11);
  }

  return codewords;
}

function splitIntoBlocks(codewords: number[], blockCount: number): number[][] {
  const blockLength = codewords.length / blockCount;

  return Array.from({ length: blockCount }, (_, index) =>
    codewords.slice(index * blockLength, (index + 1) * blockLength),
  );
}

function interleaveBlocks(blocks: number[][]): number[] {
  const maxBlockLength = Math.max(...blocks.map((block) => block.length));
  const result: number[] = [];

  for (let codewordIndex = 0; codewordIndex < maxBlockLength; codewordIndex += 1) {
    blocks.forEach((block) => {
      const codeword = block[codewordIndex];

      if (codeword !== undefined) {
        result.push(codeword);
      }
    });
  }

  return result;
}

function selectVersionSpec(payloadBytes: Uint8Array): QrVersionSpec {
  const requiredBits = 4 + 8 + payloadBytes.length * 8;
  const spec = VERSION_SPECS.find((candidate) => candidate.dataCodewords * 8 >= requiredBits);

  if (!spec) {
    throw new Error('QR payload is too long for the built-in renderer.');
  }

  return spec;
}

function isFinderPatternArea(size: number, centerX: number, centerY: number): boolean {
  const nearTop = centerY <= 10;
  const nearLeft = centerX <= 10;
  const nearRight = centerX >= size - 11;

  return nearTop && (nearLeft || nearRight) || nearLeft && centerY >= size - 11;
}

function encodeQrPayload(payload: string): EncodedQrCode {
  const payloadBytes = new TextEncoder().encode(payload);
  const spec = selectVersionSpec(payloadBytes);
  const size = spec.version * 4 + 17;
  const modules = Array.from({ length: size }, () => new Array<boolean>(size).fill(false));
  const functionModules = Array.from({ length: size }, () => new Array<boolean>(size).fill(false));

  const setFunctionModule = (x: number, y: number, isDark: boolean) => {
    if (x < 0 || y < 0 || x >= size || y >= size) {
      return;
    }

    modules[y][x] = isDark;
    functionModules[y][x] = true;
  };

  const drawFinderPattern = (left: number, top: number) => {
    for (let y = top - 1; y <= top + 7; y += 1) {
      for (let x = left - 1; x <= left + 7; x += 1) {
        const isInside = x >= left && x <= left + 6 && y >= top && y <= top + 6;
        const offsetX = x - left;
        const offsetY = y - top;
        const isDark =
          isInside &&
          (offsetX === 0 ||
            offsetX === 6 ||
            offsetY === 0 ||
            offsetY === 6 ||
            (offsetX >= 2 && offsetX <= 4 && offsetY >= 2 && offsetY <= 4));

        setFunctionModule(x, y, isDark);
      }
    }
  };

  const drawAlignmentPattern = (centerX: number, centerY: number) => {
    for (let y = centerY - 2; y <= centerY + 2; y += 1) {
      for (let x = centerX - 2; x <= centerX + 2; x += 1) {
        const distance = Math.max(Math.abs(x - centerX), Math.abs(y - centerY));
        setFunctionModule(x, y, distance !== 1);
      }
    }
  };

  drawFinderPattern(0, 0);
  drawFinderPattern(size - 7, 0);
  drawFinderPattern(0, size - 7);

  for (let index = 8; index < size - 8; index += 1) {
    const isDark = index % 2 === 0;
    setFunctionModule(index, 6, isDark);
    setFunctionModule(6, index, isDark);
  }

  spec.alignmentPatterns.forEach((centerY) => {
    spec.alignmentPatterns.forEach((centerX) => {
      if (!isFinderPatternArea(size, centerX, centerY)) {
        drawAlignmentPattern(centerX, centerY);
      }
    });
  });

  for (let index = 0; index < 8; index += 1) {
    setFunctionModule(8, index, false);
    setFunctionModule(index, 8, false);
    setFunctionModule(size - 1 - index, 8, false);
    setFunctionModule(8, size - 1 - index, false);
  }

  setFunctionModule(8, 8, false);
  setFunctionModule(8, size - 8, true);

  if (spec.version >= 7) {
    let remainder = spec.version;

    for (let index = 0; index < 12; index += 1) {
      remainder = (remainder << 1) ^ ((remainder >>> 11) * 0x1f25);
    }

    const versionBits = (spec.version << 12) | remainder;

    for (let index = 0; index < 18; index += 1) {
      const isDark = getBit(versionBits, index);
      const x = size - 11 + (index % 3);
      const y = Math.floor(index / 3);

      setFunctionModule(x, y, isDark);
      setFunctionModule(y, x, isDark);
    }
  }

  const dataCodewords = buildDataCodewords(payloadBytes, spec);
  const dataBlocks = splitIntoBlocks(dataCodewords, spec.blocks);
  const correctionBlocks = dataBlocks.map((block) =>
    createErrorCorrectionCodewords(block, spec.errorCorrectionCodewords),
  );
  const finalCodewords = [...interleaveBlocks(dataBlocks), ...interleaveBlocks(correctionBlocks)];
  const finalBits = finalCodewords.flatMap((codeword) =>
    Array.from({ length: 8 }, (_, index) => (codeword >>> (7 - index)) & 1),
  );

  let bitIndex = 0;
  let upward = true;

  for (let right = size - 1; right >= 1; right -= 2) {
    if (right === 6) {
      right -= 1;
    }

    for (let vertical = 0; vertical < size; vertical += 1) {
      const y = upward ? size - 1 - vertical : vertical;

      for (let horizontal = 0; horizontal < 2; horizontal += 1) {
        const x = right - horizontal;

        if (functionModules[y][x]) {
          continue;
        }

        const bit = finalBits[bitIndex] ?? 0;
        const mask = (x + y) % 2 === 0;
        modules[y][x] = (bit === 1) !== mask;
        bitIndex += 1;
      }
    }

    upward = !upward;
  }

  let formatRemainder = (LOW_ERROR_CORRECTION_FORMAT_BITS << 3) | MASK_PATTERN;

  for (let index = 0; index < 10; index += 1) {
    formatRemainder = (formatRemainder << 1) ^ ((formatRemainder >>> 9) * 0x537);
  }

  const formatBits = (((LOW_ERROR_CORRECTION_FORMAT_BITS << 3) | MASK_PATTERN) << 10 | formatRemainder) ^ 0x5412;

  for (let index = 0; index <= 5; index += 1) {
    setFunctionModule(8, index, getBit(formatBits, index));
  }

  setFunctionModule(8, 7, getBit(formatBits, 6));
  setFunctionModule(8, 8, getBit(formatBits, 7));
  setFunctionModule(7, 8, getBit(formatBits, 8));

  for (let index = 9; index < 15; index += 1) {
    setFunctionModule(14 - index, 8, getBit(formatBits, index));
  }

  for (let index = 0; index < 8; index += 1) {
    setFunctionModule(size - 1 - index, 8, getBit(formatBits, index));
  }

  for (let index = 8; index < 15; index += 1) {
    setFunctionModule(8, size - 15 + index, getBit(formatBits, index));
  }

  setFunctionModule(8, size - 8, true);

  return { modules, size };
}

function createSvgPath(modules: boolean[][]): string {
  return modules
    .flatMap((row, y) =>
      row.flatMap((isDark, x) => (isDark ? [`M${x + QUIET_ZONE_MODULES} ${y + QUIET_ZONE_MODULES}h1v1h-1z`] : [])),
    )
    .join('');
}

export function ScannableQrCode({ value: rawValue, title, subtitle, showHeader = true }: ScannableQrCodeProps) {
  const value = rawValue?.trim() ?? '';
  const displayTitle = title ?? t('qr.code');

  if (value === '') {
    return (
      <div className="qr-code-card qr-code-card-empty">
        <div className="panel-title token-snippet-title">{displayTitle}</div>
        <p className="empty-state">{t('qr.payloadUnavailable')}</p>
      </div>
    );
  }

  try {
    const qrCode = encodeQrPayload(value);
    const viewBoxSize = qrCode.size + QUIET_ZONE_MODULES * 2;
    const path = createSvgPath(qrCode.modules);

    return (
      <div className="qr-code-card">
        {showHeader && (
          <div>
            <div className="panel-title token-snippet-title">{displayTitle}</div>
            {subtitle && <p className="panel-subtitle">{subtitle}</p>}
          </div>
        )}
        <div className="qr-code-frame">
          <svg
            aria-label={displayTitle}
            className="qr-code-svg"
            role="img"
            viewBox={`0 0 ${viewBoxSize} ${viewBoxSize}`}
            xmlns="http://www.w3.org/2000/svg"
          >
            <rect fill="#ffffff" height={viewBoxSize} width={viewBoxSize} />
            <path d={path} fill="#020617" />
          </svg>
        </div>
      </div>
    );
  } catch (error) {
    return (
      <div className="qr-code-card qr-code-card-empty">
        <div className="panel-title token-snippet-title">{displayTitle}</div>
        <div className="form-message form-message-error">
          {error instanceof Error ? error.message : t('qr.renderError')}
        </div>
      </div>
    );
  }
}
