/**
 * Utility functions for filtering time slots based on customer preferences
 */

import { TimeSlot } from '../types';

export interface TimePreference {
  /** Earliest time acceptable (24-hour format, e.g., "15:00" for 3pm) */
  afterTime?: string;
  /** Latest time acceptable (24-hour format, e.g., "17:00" for 5pm) */
  beforeTime?: string;
}

export interface DayPreference {
  /** Days of week customer wants (0=Sunday, 1=Monday, ..., 6=Saturday) */
  daysOfWeek?: number[];
}

export interface SlotFilterPreferences extends TimePreference, DayPreference {}

/**
 * Parse time string to minutes since midnight for comparison
 * Handles formats like: "9:00am", "3:30pm", "12:00pm" (noon), "12:00am" (midnight)
 */
export function parseTimeToMinutes(timeStr: string): number {
  const match = timeStr.match(/(\d{1,2}):(\d{2})\s*(am|pm)/i);
  if (!match) return 0;

  let hours = parseInt(match[1], 10);
  const minutes = parseInt(match[2], 10);
  const meridiem = match[3].toLowerCase();

  // Convert to 24-hour format
  if (meridiem === 'pm' && hours !== 12) {
    hours += 12;
  } else if (meridiem === 'am' && hours === 12) {
    hours = 0;
  }

  return hours * 60 + minutes;
}

/**
 * Parse 24-hour time string (e.g., "15:00") to minutes since midnight
 */
export function parse24HourTime(timeStr: string): number {
  const match = timeStr.match(/(\d{1,2}):(\d{2})/);
  if (!match) return 0;

  const hours = parseInt(match[1], 10);
  const minutes = parseInt(match[2], 10);

  return hours * 60 + minutes;
}

/**
 * Filter time slots based on time preferences
 * @param slots - Array of time slots to filter
 * @param prefs - Time preferences (afterTime, beforeTime in 24-hour format)
 * @returns Filtered array of time slots
 */
export function filterSlotsByTime(
  slots: TimeSlot[],
  prefs: TimePreference
): TimeSlot[] {
  if (!prefs.afterTime && !prefs.beforeTime) {
    return slots;
  }

  const afterMinutes = prefs.afterTime ? parse24HourTime(prefs.afterTime) : 0;
  const beforeMinutes = prefs.beforeTime ? parse24HourTime(prefs.beforeTime) : 24 * 60;

  return slots.filter((slot) => {
    const slotMinutes = parseTimeToMinutes(slot.time);

    if (prefs.afterTime && slotMinutes < afterMinutes) {
      return false;
    }

    if (prefs.beforeTime && slotMinutes >= beforeMinutes) {
      return false;
    }

    return true;
  });
}

/**
 * Filter time slots based on day of week preferences
 * @param date - The date string (YYYY-MM-DD)
 * @param slots - Array of time slots for that date
 * @param prefs - Day preferences (daysOfWeek array where 0=Sunday, 1=Monday, ..., 6=Saturday)
 * @returns Filtered array of time slots (empty if date doesn't match preferred days)
 */
export function filterSlotsByDay(
  date: string,
  slots: TimeSlot[],
  prefs: DayPreference
): TimeSlot[] {
  if (!prefs.daysOfWeek || prefs.daysOfWeek.length === 0) {
    return slots;
  }

  // Parse the date - use UTC to avoid timezone shifts
  const parts = date.split('-');
  const year = parseInt(parts[0], 10);
  const month = parseInt(parts[1], 10) - 1; // 0-indexed
  const day = parseInt(parts[2], 10);
  const dateObj = new Date(Date.UTC(year, month, day));
  const dayOfWeek = dateObj.getUTCDay(); // 0=Sunday, 1=Monday, ..., 6=Saturday

  // If this day is not in the preferred days, return empty array
  if (!prefs.daysOfWeek.includes(dayOfWeek)) {
    return [];
  }

  return slots;
}

/**
 * Filter time slots based on combined preferences (day of week + time)
 * @param date - The date string (YYYY-MM-DD)
 * @param slots - Array of time slots for that date
 * @param prefs - Combined preferences (daysOfWeek, afterTime, beforeTime)
 * @returns Filtered array of time slots
 */
export function filterSlots(
  date: string,
  slots: TimeSlot[],
  prefs: SlotFilterPreferences
): TimeSlot[] {
  // First filter by day of week
  let filtered = filterSlotsByDay(date, slots, prefs);

  // Then filter by time
  filtered = filterSlotsByTime(filtered, prefs);

  return filtered;
}

/**
 * Helper to convert day names to day numbers
 * @param dayNames - Array of day names (e.g., ['Monday', 'Tuesday'])
 * @returns Array of day numbers (0=Sunday, 1=Monday, ..., 6=Saturday)
 */
export function dayNamesToNumbers(dayNames: string[]): number[] {
  const dayMap: Record<string, number> = {
    sunday: 0,
    monday: 1,
    tuesday: 2,
    wednesday: 3,
    thursday: 4,
    friday: 5,
    saturday: 6,
  };

  return dayNames
    .map((name) => dayMap[name.toLowerCase()])
    .filter((num) => num !== undefined);
}

/**
 * Example usage helper for common scenarios
 */
export const commonPreferences = {
  /** Weekdays only (Monday-Friday) */
  weekdays: { daysOfWeek: [1, 2, 3, 4, 5] },

  /** Weekends only (Saturday-Sunday) */
  weekends: { daysOfWeek: [0, 6] },

  /** Business hours (9am-5pm) */
  businessHours: { afterTime: '09:00', beforeTime: '17:00' },

  /** After work hours (after 5pm) */
  afterWork: { afterTime: '17:00' },

  /** Morning only (before noon) */
  morningOnly: { beforeTime: '12:00' },

  /** Afternoon only (after noon) */
  afternoonOnly: { afterTime: '12:00' },
};
