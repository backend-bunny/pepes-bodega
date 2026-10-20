declare global {
	namespace App {
		interface PageState {
			/** Media id shown in the lightbox (shallow routing over the results). */
			lightbox?: number;
		}
	}
}

export {};
