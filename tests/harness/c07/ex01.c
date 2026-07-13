#include <stdio.h>
#include <stdlib.h>

int *ft_range(int min, int max);

int main(int argc, char **argv)
{
	int	*range;
	int	min;
	int	max;
	int	i;

	if (argc != 3)
		return (0);
	min = atoi(argv[1]);
	max = atoi(argv[2]);
	range = ft_range(min, max);
	if (min >= max)
	{
		if (range == NULL)
			printf("NULL");
		return (0);
	}
	i = 0;
	while (i < max - min)
	{
		printf("%d", range[i]);
		if (i + 1 < max - min)
			printf(" ");
		i++;
	}
	free(range);
	return (0);
}
