#include <stdio.h>
#include <stdlib.h>

int ft_ultimate_range(int **range, int min, int max);

int main(int argc, char **argv)
{
	int	*range;
	int	size;
	int	i;

	if (argc != 3)
		return (0);
	range = NULL;
	size = ft_ultimate_range(&range, atoi(argv[1]), atoi(argv[2]));
	i = 0;
	while (i < size)
	{
		printf("%d", range[i]);
		if (i + 1 < size)
			printf(" ");
		i++;
	}
	free(range);
	return (0);
}
