/**
 * Unit tests for slot filtering utilities
 */

import {
  parseTimeToMinutes,
  parse24HourTime,
  filterSlotsByTime,
  filterSlotsByDay,
  filterSlots,
  dayNamesToNumbers,
  commonPreferences,
} from '../../src/utils/slot-filter';
import { TimeSlot } from '../../src/types';

describe('Time Parsing', () => {
  describe('parseTimeToMinutes', () => {
    it('should parse morning times correctly', () => {
      expect(parseTimeToMinutes('9:00am')).toBe(540); // 9 * 60
      expect(parseTimeToMinutes('9:30am')).toBe(570); // 9 * 60 + 30
      expect(parseTimeToMinutes('11:45am')).toBe(705); // 11 * 60 + 45
    });

    it('should parse afternoon times correctly', () => {
      expect(parseTimeToMinutes('1:00pm')).toBe(780); // 13 * 60
      expect(parseTimeToMinutes('3:30pm')).toBe(930); // 15 * 60 + 30
      expect(parseTimeToMinutes('11:30pm')).toBe(1410); // 23 * 60 + 30
    });

    it('should handle noon correctly', () => {
      expect(parseTimeToMinutes('12:00pm')).toBe(720); // 12 * 60
      expect(parseTimeToMinutes('12:30pm')).toBe(750); // 12 * 60 + 30
    });

    it('should handle midnight correctly', () => {
      expect(parseTimeToMinutes('12:00am')).toBe(0); // 0 * 60
      expect(parseTimeToMinutes('12:30am')).toBe(30); // 0 * 60 + 30
    });

    it('should be case insensitive', () => {
      expect(parseTimeToMinutes('9:00AM')).toBe(540);
      expect(parseTimeToMinutes('3:00PM')).toBe(900);
      expect(parseTimeToMinutes('9:00Am')).toBe(540);
    });
  });

  describe('parse24HourTime', () => {
    it('should parse 24-hour format correctly', () => {
      expect(parse24HourTime('09:00')).toBe(540); // 9 * 60
      expect(parse24HourTime('15:00')).toBe(900); // 15 * 60
      expect(parse24HourTime('23:30')).toBe(1410); // 23 * 60 + 30
      expect(parse24HourTime('00:00')).toBe(0); // Midnight
    });
  });
});

describe('filterSlotsByTime', () => {
  const slots: TimeSlot[] = [
    { time: '9:00am', available: true },
    { time: '11:00am', available: true },
    { time: '1:00pm', available: true },
    { time: '3:00pm', available: true },
    { time: '5:00pm', available: true },
    { time: '7:00pm', available: true },
  ];

  it('should return all slots when no preferences provided', () => {
    const filtered = filterSlotsByTime(slots, {});
    expect(filtered).toEqual(slots);
  });

  it('should filter slots after a specific time', () => {
    const filtered = filterSlotsByTime(slots, { afterTime: '15:00' }); // After 3pm
    expect(filtered).toHaveLength(3);
    expect(filtered.map((s) => s.time)).toEqual(['3:00pm', '5:00pm', '7:00pm']);
  });

  it('should filter slots before a specific time', () => {
    const filtered = filterSlotsByTime(slots, { beforeTime: '13:00' }); // Before 1pm
    expect(filtered).toHaveLength(2);
    expect(filtered.map((s) => s.time)).toEqual(['9:00am', '11:00am']);
  });

  it('should filter slots within a time range', () => {
    const filtered = filterSlotsByTime(slots, {
      afterTime: '11:00', // After 11am
      beforeTime: '17:00', // Before 5pm
    });
    expect(filtered).toHaveLength(3);
    expect(filtered.map((s) => s.time)).toEqual(['11:00am', '1:00pm', '3:00pm']);
  });

  it('should handle edge case: exactly at afterTime', () => {
    const filtered = filterSlotsByTime(slots, { afterTime: '15:00' }); // Exactly 3pm
    expect(filtered.map((s) => s.time)).toContain('3:00pm');
  });
});

describe('filterSlotsByDay', () => {
  const slots: TimeSlot[] = [
    { time: '9:00am', available: true },
    { time: '3:00pm', available: true },
  ];

  it('should return all slots when no day preferences provided', () => {
    const filtered = filterSlotsByDay('2026-02-10', slots, {}); // Monday
    expect(filtered).toEqual(slots);
  });

  it('should return slots if date matches preferred day (Monday)', () => {
    const filtered = filterSlotsByDay('2026-02-09', slots, {
      daysOfWeek: [1], // Monday (Feb 9, 2026)
    });
    expect(filtered).toEqual(slots);
  });

  it('should return empty array if date does not match preferred days', () => {
    const filtered = filterSlotsByDay('2026-02-07', slots, {
      daysOfWeek: [1, 2, 3, 4], // Monday-Thursday (Feb 7 is Friday)
    });
    expect(filtered).toEqual([]);
  });

  it('should handle multiple preferred days correctly', () => {
    // Monday (Feb 9, 2026)
    const monday = filterSlotsByDay('2026-02-09', slots, {
      daysOfWeek: [1, 2, 3, 4], // Mon-Thu
    });
    expect(monday).toEqual(slots);

    // Friday (Feb 13, 2026 - not in preference)
    const friday = filterSlotsByDay('2026-02-13', slots, {
      daysOfWeek: [1, 2, 3, 4], // Mon-Thu
    });
    expect(friday).toEqual([]);

    // Sunday (Feb 8, 2026)
    const sunday = filterSlotsByDay('2026-02-08', slots, {
      daysOfWeek: [0, 6], // Sun, Sat
    });
    expect(sunday).toEqual(slots); // Should match since Feb 8 is Sunday
  });

  it('should handle date string parsing correctly', () => {
    // Test various dates (verified via Date.UTC)
    // Feb 8, 2026 = Sunday (0), Feb 9 = Monday (1), Feb 10 = Tuesday (2), etc.
    const testCases = [
      { date: '2026-02-08', day: 0, name: 'Sunday' },
      { date: '2026-02-09', day: 1, name: 'Monday' },
      { date: '2026-02-10', day: 2, name: 'Tuesday' },
      { date: '2026-02-11', day: 3, name: 'Wednesday' },
    ];

    testCases.forEach(({ date, day }) => {
      const filtered = filterSlotsByDay(date, slots, { daysOfWeek: [day] });
      expect(filtered).toEqual(slots);
    });
  });
});

describe('filterSlots (combined)', () => {
  const slots: TimeSlot[] = [
    { time: '9:00am', available: true },
    { time: '11:00am', available: true },
    { time: '1:00pm', available: true },
    { time: '3:00pm', available: true },
    { time: '5:00pm', available: true },
  ];

  it('should apply both day and time filters', () => {
    // Monday, after 3pm
    const filtered = filterSlots('2026-02-10', slots, {
      daysOfWeek: [1, 2, 3, 4], // Mon-Thu
      afterTime: '15:00', // After 3pm
    });

    expect(filtered).toHaveLength(2);
    expect(filtered.map((s) => s.time)).toEqual(['3:00pm', '5:00pm']);
  });

  it('should return empty array if day does not match', () => {
    // Friday, after 3pm (Friday not in preferences)
    const filtered = filterSlots('2026-02-07', slots, {
      daysOfWeek: [1, 2, 3, 4], // Mon-Thu only
      afterTime: '15:00',
    });

    expect(filtered).toEqual([]);
  });

  it('should handle user scenario: Mondays-Thursdays after 3pm', () => {
    // Test data for the week (Feb 2026: 8=Sun, 9=Mon, 10=Tue, 11=Wed, 12=Thu, 13=Fri, 14=Sat)
    const testCases = [
      { date: '2026-02-08', expected: 0, name: 'Sunday' }, // Weekend
      { date: '2026-02-09', expected: 2, name: 'Monday' }, // Mon: 3pm, 5pm
      { date: '2026-02-10', expected: 2, name: 'Tuesday' }, // Tue: 3pm, 5pm
      { date: '2026-02-11', expected: 2, name: 'Wednesday' }, // Wed: 3pm, 5pm
      { date: '2026-02-12', expected: 2, name: 'Thursday' }, // Thu: 3pm, 5pm
      { date: '2026-02-13', expected: 0, name: 'Friday' }, // Not Mon-Thu
      { date: '2026-02-14', expected: 0, name: 'Saturday' }, // Weekend
    ];

    const prefs = {
      daysOfWeek: [1, 2, 3, 4], // Monday-Thursday
      afterTime: '15:00', // After 3pm
    };

    testCases.forEach(({ date, expected, name }) => {
      const filtered = filterSlots(date, slots, prefs);
      expect(filtered).toHaveLength(expected);
      if (expected > 0) {
        expect(filtered.every((s) => parseTimeToMinutes(s.time) >= 900)).toBe(true);
      }
    });
  });
});

describe('dayNamesToNumbers', () => {
  it('should convert day names to numbers', () => {
    expect(dayNamesToNumbers(['Monday'])).toEqual([1]);
    expect(dayNamesToNumbers(['Monday', 'Tuesday'])).toEqual([1, 2]);
    expect(dayNamesToNumbers(['Monday', 'Tuesday', 'Wednesday', 'Thursday'])).toEqual([
      1, 2, 3, 4,
    ]);
  });

  it('should be case insensitive', () => {
    expect(dayNamesToNumbers(['MONDAY', 'tuesday', 'WeDnEsDay'])).toEqual([1, 2, 3]);
  });

  it('should handle Sunday and Saturday', () => {
    expect(dayNamesToNumbers(['Sunday'])).toEqual([0]);
    expect(dayNamesToNumbers(['Saturday'])).toEqual([6]);
    expect(dayNamesToNumbers(['Saturday', 'Sunday'])).toEqual([6, 0]);
  });

  it('should filter out invalid day names', () => {
    expect(dayNamesToNumbers(['Monday', 'InvalidDay', 'Tuesday'])).toEqual([1, 2]);
  });
});

describe('commonPreferences', () => {
  it('should provide weekdays preference (Mon-Fri)', () => {
    expect(commonPreferences.weekdays.daysOfWeek).toEqual([1, 2, 3, 4, 5]);
  });

  it('should provide weekends preference (Sat-Sun)', () => {
    expect(commonPreferences.weekends.daysOfWeek).toEqual([0, 6]);
  });

  it('should provide business hours (9am-5pm)', () => {
    expect(commonPreferences.businessHours).toEqual({
      afterTime: '09:00',
      beforeTime: '17:00',
    });
  });

  it('should provide after work hours (after 5pm)', () => {
    expect(commonPreferences.afterWork).toEqual({
      afterTime: '17:00',
    });
  });

  it('should provide morning only (before noon)', () => {
    expect(commonPreferences.morningOnly).toEqual({
      beforeTime: '12:00',
    });
  });

  it('should provide afternoon only (after noon)', () => {
    expect(commonPreferences.afternoonOnly).toEqual({
      afterTime: '12:00',
    });
  });
});
